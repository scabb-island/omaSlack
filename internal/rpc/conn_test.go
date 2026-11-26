package rpc

import (
	"context"
	"encoding/json"
	"testing"
)

// The push path task 2.4 (events.subscribe) will build on: a handler finds
// its connection and sends notifications on it.
func TestHandlersCanPushNotificationsOnTheirConnection(t *testing.T) {
	h := Router{"subscribe": HandlerFunc(func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		conn, ok := ConnFromContext(ctx)
		if !ok {
			t.Error("no connection in the handler's context")
			return nil, nil
		}
		if err := conn.Notify("unread.summary", map[string]int{"dms": 2}); err != nil {
			t.Errorf("Notify: %v", err)
		}
		return "subscribed", nil
	})}
	c := connect(t, serve(t, NewServer(h, Options{})))
	c.hello()
	c.send(`{"jsonrpc":"2.0","id":1,"method":"subscribe"}`)

	n := c.recv()
	if n.ID != nil || n.Method != "unread.summary" || string(n.Params) != `{"dms":2}` {
		t.Fatalf("notification = %+v", n)
	}
	wantResult(t, c.recv(), `"subscribed"`)
}

func TestNotifyRejectsParamsThatAreNotJSON(t *testing.T) {
	errs := make(chan error, 1)
	h := Router{"bad": HandlerFunc(func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		conn, _ := ConnFromContext(ctx)
		errs <- conn.Notify("x", make(chan int))
		return nil, nil
	})}
	c := connect(t, serve(t, NewServer(h, Options{})))
	c.hello()
	wantResult(t, c.call(1, "bad", ""), "null") // nothing else was written first
	if err := <-errs; err == nil {
		t.Fatal("Notify accepted a channel")
	}
}

func TestConnDoneClosesWhenTheClientLeaves(t *testing.T) {
	conns := make(chan *Conn, 1)
	h := Router{"grab": HandlerFunc(func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		conn, _ := ConnFromContext(ctx)
		conns <- conn
		return nil, nil
	})}
	c := connect(t, serve(t, NewServer(h, Options{})))
	c.hello()
	c.call(1, "grab", "")
	conn := <-conns
	select {
	case <-conn.Done():
		t.Fatal("Done closed while the client is connected")
	default:
	}
	_ = c.c.Close()
	<-conn.Done()
	if err := conn.Notify("late", nil); err == nil {
		t.Fatal("Notify succeeded on a closed connection")
	}
}

func TestNoConnOutsideAHandler(t *testing.T) {
	if _, ok := ConnFromContext(context.Background()); ok {
		t.Fatal("found a connection in a bare context")
	}
}
