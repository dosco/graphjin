package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	ax "github.com/ax-llm/ax/packages/go"
)

// A wrapper must preserve Ax's incremental stream interface. Falling back to
// the slice-returning Stream method here would defer every responder chunk
// until the provider finishes.
func streamEvents(ctx context.Context, client ax.AIClient, request, options map[string]ax.Value) (ax.AxChatStream, error) {
	if streaming, ok := client.(ax.StreamingAIClient); ok {
		return streaming.StreamEvents(ctx, request, options)
	}
	values, err := client.Stream(ctx, request, options)
	if err != nil {
		return nil, err
	}
	return &valueStream{values: values}, nil
}

type valueStream struct {
	values []ax.Value
	index  int
}

func (s *valueStream) Next() bool {
	if s.index >= len(s.values) {
		return false
	}
	s.index++
	return true
}

func (s *valueStream) Value() ax.Value { return s.values[s.index-1] }
func (s *valueStream) Err() error      { return nil }
func (s *valueStream) Close() error    { return nil }

// The migration keeps existing non-streaming fixtures on Chat while all
// agents still run through StreamingForward. Connected providers opt in only
// after their incremental transport passes the first-content gate.
type streamingModeClient struct {
	ax.AIClient
	enabled bool
	// lastStatus is the HTTP status of the last failed model call, or 0.
	// Ax 25's agent StreamingForward drops the provider error from its
	// generate failure, so the harness reads the status here instead.
	lastStatus atomic.Int32
}

func (c *streamingModeClient) record(err error) error {
	var providerError ax.AxError
	if errors.As(err, &providerError) && providerError.Status > 0 {
		c.lastStatus.Store(int32(providerError.Status))
	} else {
		c.lastStatus.Store(0)
	}
	return err
}

func (c *streamingModeClient) Chat(ctx context.Context, request, options map[string]ax.Value) (ax.Value, error) {
	response, err := c.chat(ctx, request, options)
	return response, c.record(err)
}

// maxResamples bounds new samples after a model call failed without effect:
// a malformed Gemini function call or a dropped connection. One resample was
// not enough in the V4 eval.
const maxResamples = 3

func (c *streamingModeClient) chat(ctx context.Context, request, options map[string]ax.Value) (ax.Value, error) {
	response, err := c.AIClient.Chat(ctx, request, options)
	for attempt := 0; attempt < maxResamples && err != nil && ctx.Err() == nil && resampleable(err); attempt++ {
		noteResample(err)
		response, err = c.AIClient.Chat(ctx, request, options)
	}
	if err == nil {
		guardResponses([]ax.Value{response})
	}
	return response, err
}

func (c *streamingModeClient) GetFeatures(model string) map[string]ax.Value {
	features := map[string]ax.Value{}
	if provider, ok := c.AIClient.(interface {
		GetFeatures(string) map[string]ax.Value
	}); ok {
		for key, value := range provider.GetFeatures(model) {
			features[key] = value
		}
	}
	if !c.enabled {
		features["streaming"] = false
	}
	return features
}

func (c *streamingModeClient) StreamEvents(ctx context.Context, request, options map[string]ax.Value) (ax.AxChatStream, error) {
	if !c.enabled {
		response, err := c.chat(ctx, request, options)
		if c.record(err) != nil {
			return nil, err
		}
		return &valueStream{values: []ax.Value{response}}, nil
	}
	open := func() (ax.AxChatStream, error) { return streamEvents(ctx, c.AIClient, request, options) }
	if request["response_format"] != nil {
		// Structured stage output is never shown live, so read it whole and
		// sample again when it fails, even after partial content.
		values, err := drain(open)
		for attempt := 0; attempt < maxResamples && err != nil && ctx.Err() == nil && resampleable(err); attempt++ {
			noteResample(err)
			values, err = drain(open)
		}
		if err != nil {
			return nil, c.record(err)
		}
		return &valueStream{values: guardResponses(values)}, nil
	}
	stream, err := open()
	if err != nil {
		return nil, c.record(err)
	}
	return &malformedRetryStream{AxChatStream: stream, ctx: ctx, open: open}, nil
}

func drain(open func() (ax.AxChatStream, error)) ([]ax.Value, error) {
	stream, err := open()
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	var values []ax.Value
	for stream.Next() {
		values = append(values, stream.Value())
	}
	return values, stream.Err()
}

// malformedRetryStream opens the stream again when it failed retryably
// before any answer text or function call arrived.
// Thought chunks only feed progress, so repeating them is harmless.
type malformedRetryStream struct {
	ax.AxChatStream
	ctx       context.Context
	open      func() (ax.AxChatStream, error)
	yielded   bool
	resamples int
}

func (s *malformedRetryStream) Next() bool {
	if s.AxChatStream.Next() {
		s.yielded = s.yielded || visibleOutput(s.AxChatStream.Value())
		return true
	}
	err := s.AxChatStream.Err()
	if s.yielded || s.resamples >= maxResamples || err == nil || s.ctx.Err() != nil || !resampleable(err) {
		return false
	}
	s.resamples++
	noteResample(err)
	next, openErr := s.open()
	if openErr != nil {
		return false
	}
	_ = s.AxChatStream.Close()
	s.AxChatStream = next
	return s.Next()
}

func visibleOutput(value ax.Value) bool {
	chunk, _ := value.(map[string]ax.Value)
	for _, item := range items(chunk["results"]) {
		result, _ := item.(map[string]ax.Value)
		if text, _ := result["content"].(string); text != "" {
			return true
		}
		if len(items(result["function_calls"])) > 0 {
			return true
		}
	}
	return false
}

func items(value ax.Value) []ax.Value {
	switch list := value.(type) {
	case []ax.Value:
		return list
	case *ax.AxArray:
		return list.Items
	}
	return nil
}

// debugResamples prints why a call was sampled again. Gemini's finish
// message can quote model output, so it stays out of normal logs.
var debugResamples = os.Getenv("OPENNEKO_HARNESS_DEBUG") == "1"

func noteResample(err error) {
	if !debugResamples {
		return
	}
	detail := err.Error()
	if axErr, ok := ax.AsAxError(err); ok {
		raw, _ := axErr.Payload.(map[string]ax.Value)
		for _, item := range items(raw["candidates"]) {
			candidate, _ := item.(map[string]ax.Value)
			if message, _ := candidate["finishMessage"].(string); message != "" {
				detail += ": " + runePrefix(message, 2000)
			}
		}
	}
	fmt.Fprintln(os.Stderr, "resampling after", detail)
}

func resampleable(err error) bool {
	text := err.Error()
	return strings.Contains(text, "MALFORMED_FUNCTION_CALL") || strings.Contains(text, "Network Error")
}
