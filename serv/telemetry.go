package serv

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.10.0"
)

func InitTelemetry(
	c context.Context,
	exp trace.SpanExporter,
	serviceName, serviceInstanceID string,
) error {
	r1 := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(serviceName),
		semconv.ServiceInstanceIDKey.String(serviceInstanceID),
	)

	r2, err := resource.Merge(resource.Default(), r1)
	if err != nil {
		return err
	}

	provider := trace.NewTracerProvider(
		trace.WithBatcher(exp),
		trace.WithResource(r2),
		trace.WithSampler(trace.AlwaysSample()),
	)

	otel.SetTracerProvider(provider)

	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	return nil
}
