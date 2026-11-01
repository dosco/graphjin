// Package command accepts one JSON run specification and emits NDJSON lifecycle events.
package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/open-neko/openneko/apps/ax-harness/internal/agent"
)

// Execute runs one turn: it reads the run specification from input, writes
// NDJSON events to output and closes the tool sessions with cleanup. getenv
// supplies the model routes. It returns the exit code the binary uses.
func Execute(ctx context.Context, getenv func(string) string, input io.Reader, output io.Writer, tools agent.Tools, cleanup func() error) (int, error) {
	code, err := executeWithTools(ctx, getenv, input, output, tools)
	if cleanup != nil {
		if closeErr := cleanup(); closeErr != nil {
			return 1, fmt.Errorf("tool session cleanup failed: %w", closeErr)
		}
	}
	return code, err
}

func run(ctx context.Context, input io.Reader, output io.Writer) (int, error) {
	return executeWithTools(ctx, os.Getenv, input, output, agent.Tools{})
}

func executeWithTools(ctx context.Context, getenv func(string) string, input io.Reader, output io.Writer, tools agent.Tools) (int, error) {
	var spec agent.Spec
	data, readErr := io.ReadAll(io.LimitReader(input, agent.MaxInputBytes+1))
	if readErr != nil || len(data) > agent.MaxInputBytes {
		return 2, fmt.Errorf("run input exceeds limit or could not be read")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return 2, fmt.Errorf("invalid JSON run specification")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return 2, fmt.Errorf("expected exactly one run specification")
	}
	client, err := loadModelClient(getenv)
	if err != nil {
		return 2, err
	}
	encoder := json.NewEncoder(output)
	result, err := agent.RunWithTools(ctx, spec, client, tools, func(e agent.Event) error { return encoder.Encode(e) })
	if err != nil {
		return 1, fmt.Errorf("run input or event delivery failed: %w", err)
	}
	if result.Status != "completed" {
		return 1, nil
	}
	return 0, nil
}
