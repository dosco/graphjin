package serve

import (
	"context"
	"encoding/json"
)

// The config editor has the GraphJin that serve hosts check or apply the
// full text of its edited config file:
//
//	web → serve  {"type":"config","config":{"text":"...","apply":false}}
//	serve → web  {"type":"result","result":{...}}   GraphJin's update result
//	serve → web  {"type":"exit","code":0,"error":""}
//
// Only this socket reaches it: GraphJin's GraphQL and MCP APIs refuse a
// whole config, so a GraphJin token, an agent's included, cannot replace
// one. The caller writes the file once an apply succeeds.

// ConfigRequest is an edited config file to check or apply.
type ConfigRequest struct {
	Text  string `json:"text"`
	Apply bool   `json:"apply,omitempty"`
}

// ConfigEditor checks text, or with apply applies it, on the hosted GraphJin
// and returns GraphJin's update result.
type ConfigEditor func(ctx context.Context, text string, apply bool) (json.RawMessage, error)

func (s *Server) editConfig(ctx context.Context, out *frameWriter, req *ConfigRequest) {
	code := 0
	exit := Frame{Type: "exit", Code: &code}
	switch {
	case s.EditConfig == nil:
		code, exit.Error = 2, "serve hosts no GraphJin"
	case req == nil || req.Text == "":
		code, exit.Error = 2, "expected a config frame with the config text"
	default:
		result, err := s.EditConfig(ctx, req.Text, req.Apply)
		if err != nil {
			code, exit.Error = 1, err.Error()
			break
		}
		if err := out.write(Frame{Type: "result", Result: result}); err != nil {
			return
		}
	}
	_ = out.write(exit)
}
