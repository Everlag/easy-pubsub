package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
)

var port = flag.Int("port", 8080, "port to execute on")

func main() {
	flag.Parse()

	mux := NewServer(10)

	server := http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: mux.Routes(),
		// This could be useful for graceful-ish shutdown by handling SIGINT
		// in a signal handle; out of scope for this work, though.
		//
		// Stamp each request with a shared context for graceful exits
		// BaseContext: func(l net.Listener) context.Context {
		// 	return ctx
		// },
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("failed to start server: %s", err)
	}
}
