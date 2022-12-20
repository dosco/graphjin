//go:build !wasm && !tinygo

package core

import (
	"context"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type tracer struct {
	trace.Tracer
}

type span struct {
	trace.Span
}

func (t *tracer) Start(c context.Context, name string) (context.Context, span) {
	c, s := t.Tracer.Start(c, name)
	return c, span{Span: s}
}

func (s *span) End() {
	s.Span.End()
}

func (s *span) Error(err error) {
	if s.Span.IsRecording() {
		s.Span.RecordError(err)
		s.Span.SetStatus(codes.Error, err.Error())
	}
}

type stringAttr struct {
	name  string
	value string
}

func (s *span) IsRecording() bool {
	return s.Span.IsRecording()
}

func (s *span) SetAttributesString(attrs ...stringAttr) {
	as := make([]attribute.KeyValue, len(attrs))
	for _, a := range attrs {
		as = append(as, attribute.String(a.name, a.value))
	}
	s.Span.SetAttributes(as...)
}

func newTracer() tracer {
	return tracer{Tracer: otel.Tracer("graphjin.com/core")}
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
}
