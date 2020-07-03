# easy-pubsub

easy-pubsub is a toy implementation of a pubsub server.

A subscription client is provided to give a complete example.

The minimum version to run this is go 1.13 for access to the
new error handling tools.

## Testing Manually

An automatic test is icnluded that uses the standard go testing flow.
However, it can be more satisfying to poke at something with curl, so
these examples are provided.

1. Start the server
```
go build && ./easy-pubsub
```

2. Subscribe to events using curl(it may timeout without traffic)
```
curl --include \
     --no-buffer \
     --header "Connection: Upgrade" \
	 --header "Upgrade: websocket" \
	 --header "Sec-WebSocket-Key: SGVsbG8sIHdvcmxkIQ==" \
	 --header "Sec-WebSocket-Version: 13" \
     http://localhost:8080/subscribe
```
(curl sourced from github, I haven't played with websockets in awhile)
https://gist.github.com/htp/fbce19069187ec1cc486b594104f01d0

3. Publish data(content is base64 encoded)
```
curl -X POST localhost:8080/publish --data '{"content":"Zm9vCg=="}'
```