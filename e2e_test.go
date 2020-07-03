package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPubSub(t *testing.T) {

	// This test is a bit more monolithic than I would typically like
	// However, time constraints mean this is what we've got.
	//
	// An interesting direction to go here would be to make this table-based
	// with respect to the messages that we send. Testing error patterns here
	// would be troublesome and probably best left to the individual struct
	// unit tests.

	expectedMessages := []PubSubMessage{
		{
			Content: []byte("hello"),
		},
		{
			Content: []byte("world"),
		},
	}

	server := NewServer(2)
	s := httptest.NewServer(server.Routes())
	defer s.Close()

	// Create a top-level context for us to work against.
	// Provide a 200ms time budget for this test to execute for.
	//
	// Reducing this number to single digit ms may cause flakiness.
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Millisecond*200)
	defer cancel()

	// Put together a specific context for requesting our client exit.
	subscriptionCtx, subscriptionCancel := context.WithCancel(ctx)

	subscriptionURL := fmt.Sprintf("%s/%s", s.URL, "subscribe")
	// Normally, we'd set our buffer size to a few messages.
	//
	// However, this test is simplified by
	subscription, err := NewSubscription(subscriptionCtx, subscriptionURL,
		len(expectedMessages))
	require.NoError(t, err)
	// Receive client errors on a channel as serializing execution
	// avoids some downsides of the test package possibly failing in goroutines.
	clientErrs := make(chan error, 1)
	go func() {
		clientErrs <- subscription.Receive(subscriptionCtx)
	}()

	// Since publish is stateless, we can keep things easy and just perform
	// a set of synchronous POSTs to the server.
	publishURL := fmt.Sprintf("%s/%s", s.URL, "publish")
	for _, m := range expectedMessages {
		buf, err := json.Marshal(m)
		require.NoError(t, err)

		resp, err := http.Post(publishURL, "application/json", bytes.NewReader(buf))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
	}

	// Go through and ensure that we
	for _, expected := range expectedMessages {
		select {
		case found := <-subscription.MessageChan():
			require.Equal(t, expected, found)
		case <-ctx.Done():
			t.Fatalf("context completed before all messages acquired")
		}
	}

	// Cancel our client to make sure it behaves as expected
	subscriptionCancel()

	// Wait for the client to exit
	if err := <-clientErrs; err != nil {
		// Determine if we encountered the context error we expected
		//
		// I'm not terribly happy about this and would've spent some more
		// thought on this with more time :|
		errors.As(context.DeadlineExceeded, &err)
		if err != context.Canceled && err != context.DeadlineExceeded {
			require.NoError(t, err, "unexpected client error")
		}
	}
}
