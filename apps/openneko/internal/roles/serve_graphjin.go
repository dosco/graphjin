package roles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gjagent "github.com/dosco/graphjin/agent/v3"
	gjserv "github.com/dosco/graphjin/serv/v3"
	"github.com/open-neko/openneko/apps/ax-harness/serve"
)

// runServe starts the serve role. With --path (or
// OPENNEKO_SERVE_GRAPHJIN_CONFIG) naming a GraphJin config folder, serve also
// hosts that GraphJin: it serves GraphJin's HTTP API as `graphjin serve
// --path` does, and turns ask GraphJin's agent in the same process instead
// of over HTTP. GO_ENV picks the config file, as it does for GraphJin.
func runServe(args []string) {
	dir, args := graphJinPath(args)
	if dir == "" {
		dir = strings.TrimSpace(os.Getenv("OPENNEKO_SERVE_GRAPHJIN_CONFIG"))
	}
	if dir == "" {
		serve.Main(args)
		return
	}
	gj, err := startGraphJin(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "openneko serve: GraphJin unavailable: %v\n", err)
		os.Exit(2)
	}
	startSubscriptions(dir)
	serve.MainWith(args, serve.Options{Ask: graphJinAsker(gj)})
}

// graphJinPath takes --path out of args, as `graphjin serve` spells it.
func graphJinPath(args []string) (string, []string) {
	var path string
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--path" || arg == "-path":
			if i+1 < len(args) {
				path = args[i+1]
				i++
			}
		case strings.HasPrefix(arg, "--path="):
			path = strings.TrimPrefix(arg, "--path=")
		default:
			rest = append(rest, arg)
		}
	}
	return strings.TrimSpace(path), rest
}

func startGraphJin(dir string) (*gjserv.HttpService, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	conf, err := gjserv.ReadInConfig(filepath.Join(abs, gjserv.GetConfigName()))
	if err != nil {
		return nil, err
	}
	gj, err := gjserv.NewGraphJinService(conf)
	if err != nil {
		return nil, err
	}
	go func() {
		if err := gj.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "openneko serve: GraphJin stopped: %v\n", err)
			os.Exit(1)
		}
	}()
	return gj, nil
}

// askingAgent is the part of GraphJin's service the asker uses.
type askingAgent interface {
	AgentReadOnly() bool
	AskAgentWithToken(ctx context.Context, token string, req gjagent.Request) (gjagent.Response, error)
}

// graphJinAsker asks GraphJin's agent as the run's token says, and only a
// read-only agent, as OpenNeko's HTTP path requires.
func graphJinAsker(gj askingAgent) serve.Asker {
	return func(ctx context.Context, token string, req serve.AskRequest) (serve.AskResult, error) {
		if !gj.AgentReadOnly() {
			return serve.AskResult{}, errors.New("GraphJin agent is not configured read-only; OpenNeko will not delegate data questions")
		}
		history := make([]gjagent.Turn, 0, len(req.History))
		for _, turn := range req.History {
			history = append(history, gjagent.Turn{Role: turn.Role, Content: turn.Content, Status: turn.Status})
		}
		resp, err := gj.AskAgentWithToken(ctx, token, gjagent.Request{
			Instruction: req.Instruction,
			MaxSteps:    req.MaxSteps,
			History:     history,
			Observer: func(event gjagent.ActionEvent) {
				if req.OnStep == nil {
					return
				}
				step := "Data agent: " + strings.ReplaceAll(event.Tool, "_", " ")
				if event.Status == "error" || event.Error != "" {
					step += " failed, trying again"
				}
				req.OnStep(step)
			},
		})
		if err != nil {
			return serve.AskResult{}, err
		}
		raw, err := json.Marshal(resp)
		if err != nil {
			return serve.AskResult{}, err
		}
		return serve.AskResult{Response: raw, Answer: resp.Answer, Status: resp.Status, Tokens: usageTokens(resp.Usage)}, nil
	}
}

func usageTokens(usage any) int64 {
	raw, err := json.Marshal(usage)
	if err != nil {
		return 0
	}
	var totals struct {
		TotalTokens int64 `json:"total_tokens"`
	}
	_ = json.Unmarshal(raw, &totals)
	return totals.TotalTokens
}
