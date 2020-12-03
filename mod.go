package main

import (
	"os"
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
		end := start + shardLength
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

func main() {
	uri := os.Args[1]
	ctx := opCtx{"concurrently download ranges of content URL"}
	req, err := provisionDownload(uri)
	if err != nil {
		panic(err)
	}
	shards := req.shard(16)
	retrievalsPending := int32(len(shards))
	payloadSink := make(chan struct{i int; content []byte}, len(shards))
	for i, shard := range shards {
		go func(i int, shard contentShard) {	
			buf := bytes.Buffer{}
			buf.Grow(int(shard.end-shard.start))
			ctx.fck(errors.Wrap(shard.yank(&buf), 0))
			payloadSink <- struct{i int; content []byte}{i, buf.Bytes()}
			atomic.AddInt32(&retrievalsPending, -1)
			if pending := atomic.LoadInt32(&retrievalsPending); pending == 0 {
				close(payloadSink)
			}
		}(i, shard)
	}
	chunks := make([]io.Reader, len(shards))
	for payload := range payloadSink {
		flushed := len(shards) - len(chunks)
		k := payload.i - flushed
		chunks[k] = bytes.NewReader(payload.content)
		canFlush := true
		for _, istrm := range chunks[:k] {
			canFlush = istrm != nil
			if !canFlush {
				break
			}
		}
		if !canFlush {
			continue
		}
		for _, istrm := range chunks[:k] {
			_, err := io.Copy(os.Stdout, istrm)
			ctx.fck(errors.Wrap(err, 0))
		}
		chunks = chunks[k+1:]
	}
}