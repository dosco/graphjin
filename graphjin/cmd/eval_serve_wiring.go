package main

import (
	"fmt"
	"os"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/spf13/cobra"
)

// Giving each world what the way it is being driven needs.
//
// Three of these can be combined and each needs something of its own inside
// every world: a support model in front of the stages the policy is not being
// credited for, a mailbox the trainer's completions arrive through, a recorder
// collecting the calls an external agent made. A worker serves one episode at a
// time, so per-worker state belongs to exactly one episode and needs no further
// bookkeeping.

type evalServeOptions struct {
	Support  generatorFlags
	Step     bool
	External bool
}

type evalServeWiring struct {
	envFor    func(worker int) evalEnvironment
	mailboxes []*stepMailbox
	recorders []*mcpToolRecorder
}

func newEvalServeWiring(cmd *cobra.Command, size int, opts evalServeOptions) (*evalServeWiring, error) {
	wiring := &evalServeWiring{}

	// The support model, when there is one, is built once and shared: it is a
	// fixed model answering the same stages in every world, so per-world copies
	// would only multiply connections.
	var support *gjagent.Config
	if supportModelRequested(opts.Support) {
		config, label, err := resolveSupportConfig(opts.Support)
		if err != nil {
			return nil, err
		}
		support = &config
		fmt.Fprintf(cmd.OutOrStdout(),
			"Distiller and responder stages served by %s; the policy answers the executor.\n", label)
	}

	if opts.Step {
		for worker := 0; worker < size; worker++ {
			var stageSupport ax.AIClient
			if support != nil {
				client, err := gjagent.DefaultClientFactory(*support)
				if err != nil {
					return nil, fmt.Errorf("support model: %w", err)
				}
				stageSupport = client
			}
			wiring.mailboxes = append(wiring.mailboxes, newStepMailbox(stageSupport))
		}
	}
	if opts.External {
		for worker := 0; worker < size; worker++ {
			wiring.recorders = append(wiring.recorders, newMCPToolRecorder())
		}
	}

	wiring.envFor = func(worker int) evalEnvironment {
		env := evalEnvironment{StatusOut: os.Stderr}
		switch {
		case worker < len(wiring.mailboxes):
			// A step-driven world never calls a provider for the stages the
			// trainer is driving, so the factory ignores the agent's own model
			// configuration entirely.
			mailbox := wiring.mailboxes[worker]
			env.ClientFactory = func(gjagent.Config) (ax.AIClient, error) { return mailbox, nil }
		case support != nil:
			env.ClientFactory = newStageRoutingFactory(*support, os.Stderr)
		}
		if worker < len(wiring.recorders) {
			recorder := wiring.recorders[worker]
			env.MCPRecorder = recorder.record
		}
		return env
	}
	return wiring, nil
}

// attachMailboxes maps each booted world to the mailbox its service was given.
func (w *evalServeWiring) attachMailboxes(pool *evalInstancePool) map[gjeval.Instance]*stepMailbox {
	if len(w.mailboxes) == 0 {
		return nil
	}
	byInstance := map[gjeval.Instance]*stepMailbox{}
	for index, instance := range pool.instances {
		if index < len(w.mailboxes) {
			byInstance[instance] = w.mailboxes[index]
		}
	}
	return byInstance
}

func (w *evalServeWiring) attachRecorders(pool *evalInstancePool) map[gjeval.Instance]*mcpToolRecorder {
	if len(w.recorders) == 0 {
		return nil
	}
	byInstance := map[gjeval.Instance]*mcpToolRecorder{}
	for index, instance := range pool.instances {
		if index < len(w.recorders) {
			byInstance[instance] = w.recorders[index]
		}
	}
	return byInstance
}
