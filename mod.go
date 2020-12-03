package main

import (
	"os"
	"time"
	"io"
	"fmt"
	"net/http"
	"bytes"
	"sync/atomic"

	"github.com/go-errors/errors"
)

type opCtx struct {
	op string
}

func (ctx opCtx) fck(err *errors.Error) {
	if err != nil {
		err := errors.WrapPrefix(err, ctx.op, 2)
		io.WriteString(os.Stderr, err.ErrorStack())
		os.Exit(1)
	}
}

type contentShard struct { uri string; start, end int64 }

func (shard contentShard) yank(dst io.Writer) error {
	req, _ := http.NewRequest("GET", shard.uri, nil)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", shard.start, shard.end))
	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, rsp.Body)
	return err
}

type downloadProvision struct {
	uri string
	head *http.Response
}

func (target downloadProvision) shard(concurrency int) (shards []contentShard) {
	shards = make([]contentShard, concurrency)
	shardLength := target.head.ContentLength / int64(concurrency)
	for i := 0; i < concurrency; i++ {
		start := int64(i) * shardLength
		end := start + shardLength-1
		if i == concurrency-1 {
			end = target.head.ContentLength
		}
		shards[i] = contentShard{target.uri, start, end}
	}
	return
}

func provisionDownload(uri string) (*downloadProvision, *errors.Error) {
	rsp, err := http.Head(uri)
	if err != nil {
		return nil, errors.Wrap(err, 0)
	}
	if rsp.ContentLength <= 0 {
		return nil, errors.New("unknown or zero Content-Length")
	}
	if _, ok := rsp.Header["Accept-Ranges"]; !ok {
		return nil, errors.New("no Accept-Ranges header found")
	}
	if rsp.Header.Get("Accept-Ranges") != "bytes" {
		return nil, errors.New("Accept-Ranges indicates unsupported range unit or none")
	}
	return &downloadProvision{uri, rsp}, nil
}

type writerCounter struct {
	io.Writer
	progress *uint64
}

func (ostrm *writerCounter) Write(p []byte) (n int, err error) {
	n, err = ostrm.Writer.Write(p)
	if err != nil {
		return
	}
	atomic.AddUint64(ostrm.progress, uint64(n))
	return
}

func main() {
	uri := os.Args[1]
	ctx := opCtx{"concurrently download ranges of content URL"}
	req, err := provisionDownload(uri)
	ctx.fck(err)
	shards := req.shard(16)
	retrievalsPending := int32(len(shards))
	bytesRetrieved := uint64(0)
	payloadSink := make(chan struct{i int; content []byte}, len(shards))
	for i, shard := range shards {
		go func(i int, shard contentShard) {
			buf := bytes.Buffer{}
			buf.Grow(int(shard.end-shard.start))
			ctr := writerCounter{&buf, &bytesRetrieved}
			ctx.fck(errors.Wrap(shard.yank(&ctr), 0))
			payloadSink <- struct{i int; content []byte}{i, buf.Bytes()}
			atomic.AddInt32(&retrievalsPending, -1)
			if pending := atomic.LoadInt32(&retrievalsPending); pending == 0 {
				close(payloadSink)
			}
		}(i, shard)
	}
	go func() {
		for range time.NewTicker(time.Millisecond * 100).C {
			bar := []byte("[                        ]")
			status := atomic.LoadUint64(&bytesRetrieved)
			goal := req.head.ContentLength
			progress := float64(status) / float64(goal)
			for i := 1; i < int(24*progress)+1; i++ {
				bar[i] = '#'
			}
			fmt.Fprintf(os.Stderr, "%s (%.2f%%; %d/%dB)\r", string(bar), progress * 100, status, goal)
			if status == uint64(goal) {
				fmt.Println()
				return
			}
		}
	}()
	chunks := make([]io.Reader, len(shards))
	flushNext := 0
	for payload := range payloadSink {
		chunks[payload.i] = bytes.NewReader(payload.content)
		if payload.i != flushNext {
			continue
		}
		for flushNext < len(chunks) && chunks[flushNext] != nil {
			_, err := io.Copy(os.Stdout, chunks[flushNext])
			chunks[flushNext] = nil
			ctx.fck(errors.Wrap(err, 0))
			flushNext++
		}
	}
}