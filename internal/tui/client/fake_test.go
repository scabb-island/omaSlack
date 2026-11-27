package client

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/rpc"
)

func TestFakeAnswersRecordsAndPushes(t *testing.T) {
	f := NewFake(map[string]any{"protocol": 1}, map[string]Handler{
		"unread.summary": Result(map[string]int{"dms": 2}),
		"messages.send": func(p json.RawMessage) (any, error) {
			return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: "text: must not be empty"}
		},
	})
	got, err := f.Call(context.Background(), "unread.summary", nil)
	if err != nil || string(got) != `{"dms":2}` {
		t.Fatalf("unread.summary = %s, %v", got, err)
	}
	var rerr *rpc.Error
	if _, err := f.Call(context.Background(), "messages.send", map[string]string{"text": ""}); !errors.As(err, &rerr) || rerr.Code != rpc.CodeInvalidParams {
		t.Fatalf("messages.send err = %v", err)
	}
	if _, err := f.Call(context.Background(), "nope", nil); !errors.As(err, &rerr) || rerr.Code != rpc.CodeMethodNotFound {
		t.Fatalf("unknown method err = %v", err)
	}
	if calls := f.Calls(); len(calls) != 3 || calls[1].Method != "messages.send" || string(calls[1].Params) != `{"text":""}` {
		t.Fatalf("calls = %+v", calls)
	}
	if string(f.Hello()) != `{"protocol":1}` {
		t.Fatalf("hello = %s", f.Hello())
	}

	f.Push("typing", map[string]string{"conv": "C1"})
	if n := <-f.Notifications(); n.Method != "typing" || string(n.Params) != `{"conv":"C1"}` {
		t.Fatalf("notification = %+v", n)
	}
	_ = f.Close()
	_ = f.Close()
	<-f.Done()
	if _, ok := <-f.Notifications(); ok {
		t.Fatal("notifications still open after Close")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Call(ctx, "unread.summary", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled call err = %v", err)
	}
}
