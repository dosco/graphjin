package main

import (
	"context"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
)

// The glue between a flag and a world is the one piece every other test
// bypasses: the handler tests build a mailbox and hand it to the server
// directly. If the wiring gave two workers the same mailbox, or mapped a world
// to the wrong one, those tests would still pass and `env serve --step` would
// answer one world's questions with another's completions.

func wiringFor(t *testing.T, size int, opts evalServeOptions) *evalServeWiring {
	t.Helper()
	wiring, err := newEvalServeWiring(&cobra.Command{}, size, opts)
	if err != nil {
		t.Fatal(err)
	}
	return wiring
}

// clientFor runs the worker's factory the way the service would.
func clientFor(t *testing.T, wiring *evalServeWiring, worker int) ax.AIClient {
	t.Helper()
	env := wiring.envFor(worker)
	if env.ClientFactory == nil {
		return nil
	}
	client, err := env.ClientFactory(gjagent.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestStepWiringGivesEveryWorldItsOwnMailbox(t *testing.T) {
	wiring := wiringFor(t, 3, evalServeOptions{Step: true})
	if len(wiring.mailboxes) != 3 {
		t.Fatalf("expected one mailbox per world, got %d", len(wiring.mailboxes))
	}
	seen := map[ax.AIClient]bool{}
	for worker := 0; worker < 3; worker++ {
		client := clientFor(t, wiring, worker)
		if client == nil {
			t.Fatalf("worker %d got no client factory, so its calls would go to a provider", worker)
		}
		if seen[client] {
			t.Fatalf("worker %d shares a mailbox with an earlier world", worker)
		}
		seen[client] = true
		if client != ax.AIClient(wiring.mailboxes[worker]) {
			t.Fatalf("worker %d was given the wrong mailbox", worker)
		}
	}
}

// The server looks a mailbox up by the world an episode leased, so the mapping
// from booted instance to mailbox has to line up with the order they were made.
func TestWiringMapsEachWorldToItsOwnState(t *testing.T) {
	wiring := wiringFor(t, 2, evalServeOptions{Step: true, External: true})
	pool := &evalInstancePool{instances: []gjeval.Instance{
		&gjeval.StaticInstance{URL: "http://world-0"},
		&gjeval.StaticInstance{URL: "http://world-1"},
	}}
	mailboxes := wiring.attachMailboxes(pool)
	recorders := wiring.attachRecorders(pool)
	if len(mailboxes) != 2 || len(recorders) != 2 {
		t.Fatalf("mailboxes=%d recorders=%d", len(mailboxes), len(recorders))
	}
	for index, instance := range pool.instances {
		if mailboxes[instance] != wiring.mailboxes[index] {
			t.Fatalf("world %d was mapped to the wrong mailbox", index)
		}
		if recorders[instance] != wiring.recorders[index] {
			t.Fatalf("world %d was mapped to the wrong recorder", index)
		}
	}
	// And the server's lookups agree with that mapping.
	server := &envServer{mailboxes: mailboxes, recorders: recorders}
	if server.mailboxFor(pool.instances[1]) != wiring.mailboxes[1] {
		t.Fatal("the server looked up the wrong mailbox")
	}
	if server.recorderFor(pool.instances[0]) != wiring.recorders[0] {
		t.Fatal("the server looked up the wrong recorder")
	}
}

// Asking for neither surface must change nothing: no factory, no recorder, and
// the service keeps using its own configured model exactly as before.
func TestWiringIsInertWhenNothingWasAskedFor(t *testing.T) {
	clearSupportEnv(t)
	wiring := wiringFor(t, 2, evalServeOptions{})
	if len(wiring.mailboxes) != 0 || len(wiring.recorders) != 0 {
		t.Fatalf("state was created for surfaces nobody asked for: %+v", wiring)
	}
	env := wiring.envFor(0)
	if env.ClientFactory != nil {
		t.Fatal("a plain serve must not put anything in front of the agent's own model")
	}
	if env.MCPRecorder != nil {
		t.Fatal("a plain serve must not record tool calls")
	}
	if server := (&envServer{}); server.mailboxFor(&gjeval.StaticInstance{}) != nil {
		t.Fatal("a server with no mailboxes must return none")
	}
}

// External mode needs a recorder in every world and nothing in front of the
// model: the agent being graded is not GraphJin's.
func TestExternalWiringRecordsWithoutInterceptingTheModel(t *testing.T) {
	clearSupportEnv(t)
	wiring := wiringFor(t, 2, evalServeOptions{External: true})
	if len(wiring.recorders) != 2 {
		t.Fatalf("expected one recorder per world, got %d", len(wiring.recorders))
	}
	env := wiring.envFor(0)
	if env.MCPRecorder == nil {
		t.Fatal("external mode with no recorder would grade an answer with no evidence")
	}
	if env.ClientFactory != nil {
		t.Fatal("external mode must not intercept a model nobody is calling")
	}
}

// A recorder only collects while an episode owns the world; calls made between
// episodes belong to nobody and must not be credited to the next one.
func TestRecorderOnlyCollectsWhileAnEpisodeIsRunning(t *testing.T) {
	recorder := newMCPToolRecorder()
	recorder.record(servToolEvent("before"))
	if events := recorder.stop(); len(events) != 0 {
		t.Fatalf("recorded outside an episode: %+v", events)
	}
	recorder.start()
	recorder.record(servToolEvent("during"))
	events := recorder.stop()
	if len(events) != 1 || events[0].Tool != "during" {
		t.Fatalf("events = %+v", events)
	}
	// Starting again drops whatever the last episode left behind.
	recorder.start()
	recorder.record(servToolEvent("next"))
	recorder.start()
	if events := recorder.stop(); len(events) != 0 {
		t.Fatalf("a new episode inherited the previous one's calls: %+v", events)
	}
	recorder.record(servToolEvent("after"))
	if events := recorder.stop(); len(events) != 0 {
		t.Fatalf("recorded after the episode ended: %+v", events)
	}
}

// A mailbox from a previous episode must not hand its unanswered question to
// the next one, and the call it drops has to be released rather than left
// blocked on a reply that is never coming.
func TestMailboxResetReleasesWhatTheLastEpisodeLeft(t *testing.T) {
	mailbox := newStepMailbox(nil)
	values := map[string]ax.Value{"chat_prompt": []ax.Value{
		map[string]ax.Value{"role": "system", "content": "You (`executor`) write the code."},
	}}
	errs := make(chan error, 1)
	go func() {
		_, err := mailbox.Chat(context.Background(), values, nil)
		errs <- err
	}()

	// reset() takes whatever is pending without blocking, so it is retried until
	// the parked call has actually reached the mailbox.
	deadline := time.Now().Add(10 * time.Second)
	for {
		mailbox.reset()
		select {
		case err := <-errs:
			if err == nil {
				t.Fatal("a dropped call returned a completion instead of failing")
			}
			// And nothing is left for the next episode to pick up.
			select {
			case stale := <-mailbox.requests:
				t.Fatalf("a stale question survived the reset: %+v", stale)
			default:
			}
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the parked call was never released")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func servToolEvent(tool string) serv.MCPToolEvent {
	return serv.MCPToolEvent{Tool: tool, Arguments: map[string]any{"query": "query { accounts { count_id } }"}}
}
