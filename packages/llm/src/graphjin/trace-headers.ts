import { context, propagation } from "@opentelemetry/api";

/**
 * W3C trace context headers (traceparent, tracestate, baggage) for the active
 * OpenTelemetry context, so GraphJin's spans join the caller's trace. Empty
 * when telemetry is off or no span is active.
 */
export function graphjinTraceHeaders(): Record<string, string> {
  const carrier: Record<string, string> = {};
  propagation.inject(context.active(), carrier);
  return carrier;
}
