package serve

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// The model gateway makes the host's model calls that are not agent turns:
// summaries, classification, the provider test. Serve is the one process
// that reaches model providers, so web and worker need no route out.
//
//	worker → serve  {"type":"fetch","request":{"url":"https://...","method":"POST","headers":{...},"body":"..."}}
//	serve → worker  {"type":"response","status":200,"headers":{...}}
//	serve → worker  {"type":"chunk","data":"<base64>"}   repeated
//	serve → worker  {"type":"exit","code":0,"error":""}
//
// The worker's hang-up cancels the call.

// FetchRequest is one HTTP request to a model provider.
type FetchRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

const chunkBytes = 32 << 10

// Headers that describe one hop, not the call.
var hopHeaders = map[string]bool{"connection": true, "content-length": true, "host": true, "transfer-encoding": true, "keep-alive": true, "upgrade": true}

func (s *Server) fetch(parent context.Context, conn net.Conn, reader *bufio.Reader, out *frameWriter, req *FetchRequest) {
	exit := func(err error) {
		code := 0
		frame := Frame{Type: "exit", Code: &code}
		if err != nil {
			code = 1
			frame.Error = err.Error()
		}
		_ = out.write(frame)
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	// Nothing follows the fetch frame; a hang-up cancels.
	go func() {
		defer cancel()
		_, _ = io.Copy(io.Discard, reader)
	}()

	httpReq, err := fetchRequest(ctx, req)
	if err != nil {
		exit(err)
		return
	}
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		exit(errors.New("model call failed: " + redactURL(err.Error(), httpReq.URL)))
		return
	}
	defer resp.Body.Close()
	headers := map[string]string{}
	for name, values := range resp.Header {
		if !hopHeaders[strings.ToLower(name)] {
			headers[name] = strings.Join(values, ", ")
		}
	}
	if err := out.write(Frame{Type: "response", Status: resp.StatusCode, Headers: headers}); err != nil {
		return
	}
	buf := make([]byte, chunkBytes)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if err := out.write(Frame{Type: "chunk", Data: append([]byte(nil), buf[:n]...)}); err != nil {
				return
			}
		}
		if readErr == io.EOF {
			exit(nil)
			return
		}
		if readErr != nil {
			exit(errors.New("model response interrupted"))
			return
		}
	}
}

func fetchRequest(ctx context.Context, req *FetchRequest) (*http.Request, error) {
	if req == nil || req.URL == "" {
		return nil, errors.New("expected a fetch frame with a request")
	}
	target, err := url.Parse(req.URL)
	if err != nil || (target.Scheme != "https" && target.Scheme != "http") || target.Host == "" {
		return nil, errors.New("model calls must go to an http or https URL")
	}
	method := strings.ToUpper(req.Method)
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if req.Body != "" {
		body = strings.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, err
	}
	for name, value := range req.Headers {
		if !hopHeaders[strings.ToLower(name)] {
			httpReq.Header.Set(name, value)
		}
	}
	return httpReq, nil
}

// redactURL drops the query string, where some providers take the key, from
// an error that quotes the URL.
func redactURL(message string, target *url.URL) string {
	if target.RawQuery == "" {
		return message
	}
	return strings.ReplaceAll(message, target.String(), target.Scheme+"://"+target.Host+target.Path)
}
