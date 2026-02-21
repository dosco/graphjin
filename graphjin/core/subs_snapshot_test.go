package core

import (
	"crypto/sha256"
	"encoding/json"
	"testing"
)

func TestSubSnapshotMembersIsolatedFromLiveState(t *testing.T) {
	dh1 := sha256.Sum256([]byte(`{"chats":[{"id":1}]}`))
	dh2 := sha256.Sum256([]byte(`{"chats":[{"id":2}]}`))

	res1 := make(chan *Result, 1)
	res2 := make(chan *Result, 1)

	s := &sub{
		mval: mval{
			params: []json.RawMessage{
				json.RawMessage(`["chat","cursor-1"]`),
				json.RawMessage(`["chat","cursor-2"]`),
			},
			mi: []minfo{
				{
					dh:     dh1,
					values: []interface{}{"chat", "cursor-1"},
					cindxs: []int{1},
				},
				{
					dh:     dh2,
					values: []interface{}{"chat", "cursor-2"},
					cindxs: []int{1},
				},
			},
			res: []chan *Result{res1, res2},
			ids: []uint64{101, 102},
		},
	}

	mv := s.snapshotMembers()

	// Mutate live state after snapshot creation.
	s.mi[0].dh = sha256.Sum256([]byte(`{"chats":[{"id":99}]}`))
	s.mi[0].values[1] = "cursor-99"
	s.mi[0].cindxs[0] = 9
	s.params[0] = json.RawMessage(`["chat","cursor-99"]`)
	s.ids[0] = 999
	s.res[0] = make(chan *Result, 1)

	if mv.mi[0].dh != dh1 {
		t.Fatalf("snapshot hash changed: got %x want %x", mv.mi[0].dh, dh1)
	}

	gotCursor, ok := mv.mi[0].values[1].(string)
	if !ok || gotCursor != "cursor-1" {
		t.Fatalf("snapshot values changed: got %v want %q", mv.mi[0].values[1], "cursor-1")
	}

	if mv.mi[0].cindxs[0] != 1 {
		t.Fatalf("snapshot cindxs changed: got %d want %d", mv.mi[0].cindxs[0], 1)
	}

	if got, want := string(mv.params[0]), `["chat","cursor-1"]`; got != want {
		t.Fatalf("snapshot params changed: got %s want %s", got, want)
	}

	if mv.ids[0] != 101 {
		t.Fatalf("snapshot id changed: got %d want %d", mv.ids[0], 101)
	}

	if mv.res[0] != res1 {
		t.Fatal("snapshot result channel changed")
	}
}
