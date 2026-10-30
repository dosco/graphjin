package serve

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func configResult(t *testing.T, c *client) (json.RawMessage, Frame) {
	t.Helper()
	var result json.RawMessage
	timeout := time.After(10 * time.Second)
	for {
		select {
		case frame, ok := <-c.frames:
			if !ok {
				t.Fatal("serve hung up before exit")
			}
			if frame.Type == "result" {
				result = frame.Result
			}
			if frame.Type == "exit" {
				return result, frame
			}
		case <-timeout:
			t.Fatal("no exit frame")
		}
	}
}

func TestServeChecksAndAppliesAnEditedConfig(t *testing.T) {
	type call struct {
		text  string
		apply bool
	}
	var calls []call
	socket := startServer(t, &Server{EditConfig: func(_ context.Context, text string, apply bool) (json.RawMessage, error) {
		calls = append(calls, call{text, apply})
		return json.RawMessage(`{"success":true,"valid":true}`), nil
	}})
	for _, apply := range []bool{false, true} {
		result, exit := configResult(t, dial(t, socket, Frame{Type: "config", Config: &ConfigRequest{Text: "sources: []\n", Apply: apply}}))
		if exit.Code == nil || *exit.Code != 0 || string(result) != `{"success":true,"valid":true}` {
			t.Fatalf("apply=%v: result = %s exit = %+v", apply, result, exit)
		}
	}
	if len(calls) != 2 || calls[0] != (call{"sources: []\n", false}) || calls[1] != (call{"sources: []\n", true}) {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestServeRefusesConfigItCannotEdit(t *testing.T) {
	_, exit := configResult(t, dial(t, startServer(t, &Server{}), Frame{Type: "config", Config: &ConfigRequest{Text: "a: 1\n"}}))
	if exit.Code == nil || *exit.Code == 0 || exit.Error == "" {
		t.Fatalf("without a hosted GraphJin: exit = %+v", exit)
	}
	socket := startServer(t, &Server{EditConfig: func(context.Context, string, bool) (json.RawMessage, error) {
		return nil, errors.New("GraphJin is reloading")
	}})
	if _, exit := configResult(t, dial(t, socket, Frame{Type: "config"})); exit.Code == nil || *exit.Code == 0 {
		t.Fatalf("without text: exit = %+v", exit)
	}
	if _, exit := configResult(t, dial(t, socket, Frame{Type: "config", Config: &ConfigRequest{Text: "a: 1\n"}})); exit.Error != "GraphJin is reloading" {
		t.Fatalf("on an error: exit = %+v", exit)
	}
}
