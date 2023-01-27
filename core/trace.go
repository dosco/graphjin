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

func (t *tracer) Start(c context.Context, name string) (context.Context, Spaner) {
	return c, &span{}
}

func (t *tracer) NewHTTPClient() *http.Client {
	return &http.Client{}
}

func (s *span) End() {
}

func (s *span) Error(err error) {
}

type StringAttr struct {
	Name  string
	Value string
}

func (s *span) IsRecording() bool {
	return false
}

func (s *span) SetAttributesString(attrs ...StringAttr) {
}
