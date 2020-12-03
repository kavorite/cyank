# cyank

It can take a long while to download files from the Internet. But, if the server that you're downloading from supports [ranges][http-ranges], and particularly if the resource you're accessing is backed by a load-balancer rather than a single server, `cyank` (**c**oncurrent **yank**) can probably help. It shards the request for the resource given on its first command line argument into sixteen separate such requests for contiguous fragments of the resource, and dumps them to stdout as they become available, such that the requested payload arrives intact. Invoke it like so:

```
$ ./cyank http://files.grouplens.org/datasets/movielens/ml-25m.zip > output.zip
# or whatever else, it works with content hosted in frankly startling spots-- I used imgur to test
```

Although it does check explicitly that the remote supports range-requests, it currently does _not_ poll the remote host more than once per shard in attempts to guarantee that your requests will be serviced by separate IPs. It does seem to provide a good amount of speed-up when I race my browser against it despite this, however (2x, against the URL above). I'm as mystified as you are. Enjoy.

[http-ranges]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Range_requests
