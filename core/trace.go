package core

import (
	"context"
	"net/http"
)

type Tracer interface {
	Start(c context.Context, name string) (context.Context, Spaner)
	NewHTTPClient() *http.Client
}

type Spaner interface {
	SetAttributesString(attrs ...StringAttr)
	IsRecording() bool
	Error(err error)
	End()
}

type tracer struct{}

type span struct{}

// Start starts a new trace span
func (t *tracer) Start(c context.Context, name string) (context.Context, Spaner) {
	return c, &span{}
}

// NewHTTPClient creates a new HTTP client
func (t *tracer) NewHTTPClient() *http.Client {
	return &http.Client{}
}

// End ends the span
func (s *span) End() {
}

// Error logs an error
func (s *span) Error(err error) {
}

type StringAttr struct {
	Name  string
	Value string
}

// IsRecording returns true if the span is recording
func (s *span) IsRecording() bool {
	return false
}

// SetAttributesString sets the attributes
func (s *span) SetAttributesString(attrs ...StringAttr) {
}
