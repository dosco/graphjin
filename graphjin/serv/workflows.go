package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	workflowsPath         = "workflows"
	workflowExt           = ".js"
	workflowScriptTimeout = 5 * time.Second
)

// apiV1Workflows handles REST execution of named JS workflows.
// Route format: /api/v1/workflows/<name>
func (s1 *HttpService) apiV1Workflows(ns *string) http.Handler {
	rLen := len(routeWorkflows)

	h := func(w http.ResponseWriter, r *http.Request) {
		s := s1.Load().(*graphjinService)
		w.Header().Set("Content-Type", "application/json")

		if len(r.RequestURI) < rLen {
			renderErr(w, errors.New("no workflow name defined"))
			return
		}

		workflowName := r.RequestURI[rLen-1:]
		if n := strings.IndexRune(workflowName, '?'); n != -1 {
			workflowName = workflowName[:n]
		}

		input, err := parseWorkflowInput(r)
		if err != nil {
			renderErr(w, err)
			return
		}

		result, err := s.runNamedWorkflow(r.Context(), workflowName, input, ns)
		if err != nil {
			renderErr(w, err)
			return
		}

		if err := json.NewEncoder(w).Encode(map[string]any{"data": result}); err != nil {
			renderErr(w, err)
		}
	}

	return http.HandlerFunc(h)
}

func parseWorkflowInput(r *http.Request) (any, error) {
	switch r.Method {
	case http.MethodPost:
		b, err := parseBody(r)
		if err != nil {
			return nil, err
		}
		if len(strings.TrimSpace(string(b))) == 0 {
			return map[string]any{}, nil
		}

		var input any
		if err := json.Unmarshal(b, &input); err != nil {
			return nil, fmt.Errorf("invalid request body JSON: %w", err)
		}
		return input, nil

	case http.MethodGet:
		vars := strings.TrimSpace(r.URL.Query().Get("variables"))
		if vars == "" {
			return map[string]any{}, nil
		}

		var input any
		if err := json.Unmarshal([]byte(vars), &input); err != nil {
			return nil, fmt.Errorf("invalid variables JSON: %w", err)
		}
		return input, nil

	default:
		return nil, fmt.Errorf("unsupported method %q (use GET or POST)", r.Method)
	}
}

func (s *graphjinService) runNamedWorkflow(ctx context.Context, name string, input any, ns *string) (any, error) {
	normName, err := normalizeWorkflowName(name)
	if err != nil {
		return nil, err
	}

	scriptFile := filepath.Join(workflowsPath, normName+workflowExt)
	src, err := s.fs.Get(scriptFile)
	if err != nil {
		return nil, fmt.Errorf("workflow not found: %s", normName)
	}

	ms := s.newMCPServerWithContext(ctx)
	return ms.runWorkflowScript(ctx, normName, string(src), input, ns)
}

func normalizeWorkflowName(name string) (string, error) {
	name = strings.TrimSpace(strings.TrimPrefix(name, "/"))
	if name == "" {
		return "", errors.New("workflow name is required")
	}

	if strings.HasSuffix(strings.ToLower(name), workflowExt) {
		name = name[:len(name)-len(workflowExt)]
	}

	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("invalid workflow name: %q", name)
	}

	return name, nil
}

func (ms *mcpServer) runWorkflowScript(ctx context.Context, workflowName, script string, input any, ns *string) (any, error) {
	vm := goja.New()
	done := make(chan struct{})

	timer := time.AfterFunc(workflowScriptTimeout, func() {
		vm.Interrupt(fmt.Errorf("workflow execution exceeded %s", workflowScriptTimeout))
	})
	defer timer.Stop()

	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(ctx.Err())
		case <-done:
		}
	}()
	defer close(done)

	if input == nil {
		input = map[string]any{}
	}

	if err := vm.Set("input", input); err != nil {
		return nil, err
	}
	if err := vm.Set("ctx", workflowContext(ctx, ns)); err != nil {
		return nil, err
	}

	console := vm.NewObject()
	console.Set("log", func(args ...any) {
		if ms.service.log != nil {
			ms.service.log.Debugw("workflow console.log", "workflow", workflowName, "args", args)
		}
	}) //nolint:errcheck
	if err := vm.Set("console", console); err != nil {
		return nil, err
	}

	gj, err := ms.newWorkflowGlobals(vm, ctx, ns)
	if err != nil {
		return nil, err
	}
	if err := vm.Set("gj", gj); err != nil {
		return nil, err
	}

	val, err := vm.RunScript(workflowName+workflowExt, script)
	if err != nil {
		return nil, err
	}

	mainFn, ok := goja.AssertFunction(vm.Get("main"))
	if ok {
		v, err := mainFn(goja.Undefined(), vm.ToValue(input))
		if err != nil {
			return nil, err
		}
		return v.Export(), nil
	}

	if !goja.IsUndefined(val) && !goja.IsNull(val) {
		return val.Export(), nil
	}

	resultVal := vm.Get("result")
	if !goja.IsUndefined(resultVal) && !goja.IsNull(resultVal) {
		return resultVal.Export(), nil
	}

	return nil, nil
}

func (ms *mcpServer) newWorkflowGlobals(vm *goja.Runtime, ctx context.Context, ns *string) (*goja.Object, error) {
	gj := vm.NewObject()
	tools := vm.NewObject()

	callTool := func(call goja.FunctionCall) goja.Value {
		toolName, args := parseToolCall(vm, call)
		out, err := ms.invokeToolForWorkflow(ctx, toolName, args, ns)
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		return vm.ToValue(out)
	}

	if err := tools.Set("call", callTool); err != nil {
		return nil, err
	}

	toolMap := ms.srv.ListTools()
	toolNames := make([]string, 0, len(toolMap))
	for name := range toolMap {
		if name == "get_js_runtime_api" || name == "execute_workflow" {
			continue
		}
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)

	for _, toolName := range toolNames {
		toolName := toolName
		fnName := snakeToCamel(toolName)
		fn := func(call goja.FunctionCall) goja.Value {
			args := map[string]any{}
			if len(call.Arguments) > 0 && !goja.IsUndefined(call.Argument(0)) && !goja.IsNull(call.Argument(0)) {
				exported := call.Argument(0).Export()
				if exported != nil {
					var ok bool
					args, ok = exported.(map[string]any)
					if !ok {
						panic(vm.ToValue("tool arguments must be an object"))
					}
				}
			}

			out, err := ms.invokeToolForWorkflow(ctx, toolName, args, ns)
			if err != nil {
				panic(vm.ToValue(err.Error()))
			}
			return vm.ToValue(out)
		}
		if err := tools.Set(fnName, fn); err != nil {
			return nil, err
		}
	}

	meta := vm.NewObject()
	if err := meta.Set("listFunctions", func() []JSRuntimeFunction {
		return ms.buildJSRuntimeAPI().Functions
	}); err != nil {
		return nil, err
	}

	if err := gj.Set("tools", tools); err != nil {
		return nil, err
	}
	if err := gj.Set("meta", meta); err != nil {
		return nil, err
	}

	return gj, nil
}

func parseToolCall(vm *goja.Runtime, call goja.FunctionCall) (string, map[string]any) {
	if len(call.Arguments) == 0 || goja.IsUndefined(call.Argument(0)) || goja.IsNull(call.Argument(0)) {
		panic(vm.ToValue("tool name is required"))
	}

	toolName := call.Argument(0).String()
	args := map[string]any{}

	if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) && !goja.IsNull(call.Argument(1)) {
		exported := call.Argument(1).Export()
		if exported != nil {
			var ok bool
			args, ok = exported.(map[string]any)
			if !ok {
				panic(vm.ToValue("tool arguments must be an object"))
			}
		}
	}

	return toolName, args
}

func workflowContext(ctx context.Context, ns *string) map[string]any {
	data := map[string]any{
		"user_id":   ctx.Value(core.UserIDKey),
		"user_role": ctx.Value(core.UserRoleKey),
	}
	if ns != nil {
		data["namespace"] = *ns
	}
	return data
}

func (ms *mcpServer) invokeToolForWorkflow(
	ctx context.Context,
	toolName string,
	args map[string]any,
	ns *string,
) (any, error) {
	if toolName == "execute_workflow" {
		return nil, errors.New("execute_workflow cannot be called from workflow runtime")
	}

	tool, ok := ms.srv.ListTools()[toolName]
	if !ok {
		return nil, fmt.Errorf("unknown workflow tool: %s", toolName)
	}

	callArgs := map[string]any{}
	for k, v := range args {
		callArgs[k] = v
	}

	if ns != nil && *ns != "" {
		if _, ok := tool.Tool.InputSchema.Properties["namespace"]; ok {
			if v, ok := callArgs["namespace"].(string); !ok || v == "" {
				callArgs["namespace"] = *ns
			}
		}
	}

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: callArgs,
		},
	}

	res, err := tool.Handler(ctx, req)
	if err != nil {
		return nil, err
	}
	return decodeWorkflowToolResult(res)
}

func decodeWorkflowToolResult(res *mcp.CallToolResult) (any, error) {
	if res == nil {
		return nil, nil
	}

	if res.IsError {
		return nil, errors.New(workflowToolErrorMessage(res))
	}

	if res.StructuredContent != nil {
		return res.StructuredContent, nil
	}

	if len(res.Content) == 0 {
		return nil, nil
	}

	if text, ok := res.Content[0].(mcp.TextContent); ok {
		t := strings.TrimSpace(text.Text)
		if t == "" {
			return "", nil
		}
		var decoded any
		if err := json.Unmarshal([]byte(t), &decoded); err == nil {
			return decoded, nil
		}
		return t, nil
	}

	b, err := json.Marshal(res.Content[0])
	if err != nil {
		return res.Content[0], nil
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return string(b), nil
	}
	return out, nil
}

func workflowToolErrorMessage(res *mcp.CallToolResult) string {
	if len(res.Content) > 0 {
		if text, ok := res.Content[0].(mcp.TextContent); ok && strings.TrimSpace(text.Text) != "" {
			return text.Text
		}
	}

	if res.StructuredContent != nil {
		b, err := json.Marshal(res.StructuredContent)
		if err == nil {
			return string(b)
		}
	}

	return "workflow tool call failed"
}
