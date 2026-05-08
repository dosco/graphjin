package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"go.uber.org/zap"
)

// isSSERequest reports whether the client requested a Server-Sent Events
// stream via the Accept header.
func isSSERequest(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

// clearStreamDeadline removes the per-request read/write deadlines for
// long-lived streams (SSE, WebSocket). The global http.Server WriteTimeout
// would otherwise terminate the connection after a few seconds.
func clearStreamDeadline(w http.ResponseWriter) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	_ = rc.SetReadDeadline(time.Time{})
}

// apiV1SSE streams subscription results to the client using Server-Sent Events.
// Implements the GraphQL-SSE protocol's "distinct connections mode": one
// subscription per HTTP request. Each result is written as a `next` event;
// the stream is terminated with a `complete` event when the subscription ends.
//
// Reference: https://github.com/graphql/graphql-over-http/blob/main/rfcs/GraphQLOverSSE.md
func (s *graphjinService) apiV1SSE(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	req gqlReq,
	rc *core.RequestConfig,
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	if err := s.checkGraphJinInitialized(); err != nil {
		renderErr(w, err)
		return
	}

	clearStreamDeadline(w)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	m, err := s.gj.Subscribe(ctx, req.Query, req.Vars, rc)
	if err != nil {
		writeSSEError(w, flusher, err)
		writeSSEComplete(w, flusher)
		return
	}
	defer m.Unsubscribe()

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)

	// Periodic ping to keep intermediaries (proxies, load balancers) from
	// idling out the connection on quiet subscriptions.
	pingTick := time.NewTicker(30 * time.Second)
	defer pingTick.Stop()

	for {
		select {
		case v, ok := <-m.Result:
			if !ok {
				writeSSEComplete(w, flusher)
				return
			}

			res := Payload{Data: v.Data, Errors: v.Errors}
			buf.Reset()
			if err := enc.Encode(res); err != nil {
				s.zlog.Error("SSE encode", zap.Error(err))
				return
			}
			// enc.Encode appends '\n'; SSE wants a blank line between
			// events, so we keep that newline and add one more below.
			if _, err := w.Write([]byte("event: next\ndata: ")); err != nil {
				return
			}
			if _, err := w.Write(buf.Bytes()); err != nil {
				return
			}
			if _, err := w.Write([]byte{'\n'}); err != nil {
				return
			}
			flusher.Flush()

		case <-pingTick.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()

		case <-ctx.Done():
			return
		}
	}
}

// writeSSEError emits an SSE `next` event carrying a GraphQL error payload.
func writeSSEError(w http.ResponseWriter, flusher http.Flusher, err error) {
	res := Payload{Errors: []core.Error{{Message: err.Error()}}}
	body, mErr := json.Marshal(res)
	if mErr != nil {
		return
	}
	_, _ = w.Write([]byte("event: next\ndata: "))
	_, _ = w.Write(body)
	_, _ = w.Write([]byte("\n\n"))
	flusher.Flush()
}

// writeSSEComplete emits the terminating SSE `complete` event.
func writeSSEComplete(w http.ResponseWriter, flusher http.Flusher) {
	_, _ = w.Write([]byte("event: complete\ndata: \n\n"))
	flusher.Flush()
}
