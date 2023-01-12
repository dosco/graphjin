//go:build wasm

package core

import (
	"context"
	"net/http"
)

type tracer struct {
}

type span struct {
}

func (t *tracer) Start(c context.Context, name string) (context.Context, span) {
	return c, span{}
}

func (s *span) End() {
}

func (s *span) Error(err error) {
}

type stringAttr struct {
	name  string
	value string
}

func (s *span) IsRecording() bool {
	return false
}

func (s *span) SetAttributesString(attrs ...stringAttr) {
}

func newTracer() tracer {
	return tracer{}
}

func newHTTPClient() *http.Client {
	return &http.Client{}
}
