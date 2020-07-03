package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/pkg/errors"
	"nhooyr.io/websocket"
)

// Server implements an in-memory pubsub server that works
// over websockets.
type Server struct {
	subscribers *subscriberSet
}

// NewServer returns a Server
func NewServer(backlogLimit int) *Server {
	return &Server{
		subscribers: newSubscriberSet(backlogLimit),
	}
}

// ErrorHandler is an http.HandlerFunc that can return an error so
// it can be tracked.
type ErrorHandler func(w http.ResponseWriter, r *http.Request) error

func (s *Server) subscribeHandler(w http.ResponseWriter, r *http.Request) error {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		// No need to write an error to the client since Accept does that for us
		return errors.Wrap(err, "upgrading to websocket connection")
	}
	// To allow us to perform early returns, we default to Closing with an error
	// status. Since Close nops after the first call, we can also call Close
	// prior to exiting with a more semantic code in a non-error case.
	//
	// Essentially, like tx.Rollback and tx.Commit when handling DB calls :)
	defer ws.Close(websocket.StatusInternalError, "the sky is falling")

	subscriptionID, messageChan := s.subscribers.Add()
	defer s.subscribers.Remove(subscriptionID)

	ctx := r.Context()

	for m := range messageChan {
		writer, err := ws.Writer(ctx, websocket.MessageText)
		if err != nil {
			return errors.Wrap(err, "getting websocket writer")
		}
		err = json.NewEncoder(writer).Encode(m)
		if err != nil {
			return errors.Wrap(err, "sending remote message")
		}
		if err := writer.Close(); err != nil {
			return errors.Wrap(err, "closing message writer")
		}
	}

	// If we can exit cleanly then we should.
	ws.Close(websocket.StatusNormalClosure, "")
	return nil
}

func (s *Server) publishHandler(w http.ResponseWriter, r *http.Request) error {
	// We check the method; typically, this would be done at the router level.
	// However, we don't get for free some ServeMux like we would from something
	// like chi so we'll do that checking in our handler.
	if r.Method != http.MethodPost {
		http.Error(w, "publish may only be performed via POST", http.StatusMethodNotAllowed)
	}

	var m PubSubMessage
	err := json.NewDecoder(r.Body).Decode(&m)
	if err != nil {
		return errors.Wrap(err, "parsing PubSubMessage json")
	}

	s.subscribers.Broadcast(m)

	if _, err := w.Write([]byte("okay")); err != nil {
		return errors.Wrap(err, "sending reply")
	}
	return nil
}

func serverErrorMuxer(h ErrorHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := h(w, r)
		if err != nil {
			// If the remote gracefully hungup, we can also gracefully ignore them.
			//
			// It might make sense to record a metric here...
			status := websocket.CloseStatus(err)
			if status == websocket.StatusNormalClosure {
				return
			}

			// Normally, handling errors as a server, we'd do something like
			// accumulate to a metric here or perform some structured logging.
			//
			// However, to avoid bloating this minimal implementation
			// we're going to do a simple log statement.
			log.Println("server-side error", err)
		}
	}
}

// Routes returns an http.Handler that exposes the Server
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/subscribe", serverErrorMuxer(s.subscribeHandler))
	mux.Handle("/publish", serverErrorMuxer(s.publishHandler))

	return mux
}

type subscriberSet struct {
	backlogBufferSize int

	index uint
	set   map[uint]chan PubSubMessage

	sync.Mutex
}

func newSubscriberSet(backlogLimit int) *subscriberSet {
	return &subscriberSet{
		backlogBufferSize: backlogLimit,
		set:               make(map[uint]chan PubSubMessage, 0),
	}
}

func (s *subscriberSet) Add() (uint, chan PubSubMessage) {
	s.Lock()
	defer s.Unlock() // Much faster in go 1.14 :D

	// TODO: configurable internal buffer size
	buffer := make(chan PubSubMessage, s.backlogBufferSize)
	id := s.index
	s.set[id] = buffer

	s.index++

	return id, buffer
}

func (s *subscriberSet) Remove(id uint) {
	s.Lock()
	defer s.Unlock()

	// Close the channel to let the subscriber know that they're done
	close(s.set[id])
	delete(s.set, id)
}

func (s *subscriberSet) Broadcast(m PubSubMessage) {
	s.Lock()
	defer s.Unlock()

	for _, ch := range s.set {
		select {
		case ch <- m:
		default:
			// The semantic I'm choosing to go with is a client that accumulates
			// beyond the allowed amount of lag has their messages dropped.
			//
			// Hence, we skip channel writes that would block us. In a situation
			// where the subscriber MUST see all writes, a mailbox based approach
			// or a more-shared store of messages would also work.
		}
	}
}
