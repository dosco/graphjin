package otel

import (
	"context"
	"net/http"

	"github.com/dosco/graphjin/core/v3"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Tracer struct {
	trace.Tracer
}

type span struct {
	trace.Span
}

func NewTracer() *Tracer {
	return &Tracer{Tracer: otel.Tracer("graphjin.com/core")}
}

func NewTracerFrom(t trace.Tracer) *Tracer {
	return &Tracer{Tracer: t}
}

func (t *Tracer) Start(c context.Context, name string) (context.Context, core.Spaner) {
	c, s := t.Tracer.Start(c, name)
	return c, &span{Span: s}
}

func (t *Tracer) NewHTTPClient() *http.Client {
	return &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
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

func (s *span) IsRecording() bool {
	return s.Span.IsRecording()
}

func (s *span) SetAttributesString(attrs ...core.StringAttr) {
	as := make([]attribute.KeyValue, len(attrs))
	for _, a := range attrs {
		as = append(as, attribute.String(a.Name, a.Value))
	}
	s.Span.SetAttributes(as...)
}
