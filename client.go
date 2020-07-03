package main

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/pkg/errors"
	"nhooyr.io/websocket"
)

// PubSubMessage is the base wrapper for all messages received and sent by
// a PubSub service.
//
// We work with structured data rather than just shipping blobs around
// in case we wish to add signalling to messages in the future, ie standardized
// metadata.
type PubSubMessage struct {
	Content []byte `json:"content"`
}

// SubscriptionHandle is a wrapper for reading from a PubSubServer
type SubscriptionHandle struct {
	ws     *websocket.Conn
	buffer chan PubSubMessage

	sync.Mutex
}

// MessageChan returns a channel that will receive messages
// encountered while Subscribe is executing.
func (s *SubscriptionHandle) MessageChan() <-chan PubSubMessage {
	return s.buffer
}

func (s *SubscriptionHandle) readMessage(ctx context.Context,
	ws *websocket.Conn) (PubSubMessage, error) {

	msgType, buf, err := ws.Read(ctx)
	if err != nil {
		return PubSubMessage{},
			errors.Wrap(err, "failed reading from remote")
	}

	if msgType != websocket.MessageText {
		return PubSubMessage{},
			errors.Errorf("received non-text message type: %s", msgType)
	}

	var msg PubSubMessage
	if err := json.Unmarshal(buf, &msg); err != nil {
		return PubSubMessage{}, errors.Wrap(err, "decoding remote message")
	}

	return msg, nil
}

// Receive listens for messages from the server and sends them to the
// SubscriptionHandle's MessageChan after they've been received.
//
// Receive MAY ONLY be invoked once as the subscription is terminated
// immediately after this function returns.
func (s *SubscriptionHandle) Receive(ctx context.Context) error {

	// Alias for easier referenceing below
	ws := s.ws

	// Queue up cleanup operations to provide 'handle' semantics.
	//
	// Close the buffer. There is a tradeoff here; we could not close the buffer
	// but that would prevent users from ranging over our MessageChan.
	defer close(s.buffer)
	// Disconnect from the server
	defer ws.Close(websocket.StatusNormalClosure, "Client closing")

	for {
		msg, err := s.readMessage(ctx, ws)
		if err != nil {
			// Handle the case of a regular remote hangup
			status := websocket.CloseStatus(err)
			if status == websocket.StatusNormalClosure {
				return nil
			}
			return errors.Wrap(err, "reading message")
		}

		select {
		case s.buffer <- msg:
		case <-ctx.Done():
			return errors.Wrap(err, "context finished before message could be sent")
		}
	}
}

// NewSubscription returns a new SubscriptionHandle connected to the remote server
func NewSubscription(ctx context.Context, remote string,
	bufferedMessages int) (*SubscriptionHandle, error) {

	ws, _, err := websocket.Dial(ctx, remote, nil)
	if err != nil {
		return nil, errors.Wrap(err, "dialing websocket")
	}

	return &SubscriptionHandle{
		buffer: make(chan PubSubMessage, bufferedMessages),
		ws:     ws,
	}, nil
}
