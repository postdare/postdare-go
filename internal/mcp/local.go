package mcp

import (
	"bytes"
	"io"
	"net/http"
	"time"
)

// localBaseURL is only the Host the in-process requests carry. Nothing dials
// it: localRoundTripper hands every request straight to the router.
const localBaseURL = "http://postdare-go.local"

// NewLocalServer builds a server whose REST calls are served by handler inside
// this process. The Streamable HTTP endpoint uses it so a tool call never
// leaves the machine, while still passing through the router's auth and
// mutation gates exactly as the stdio client's network calls do. The endpoint
// already requires the MCP token, so authenticating the loopback call with the
// same token keeps the actor identity the handlers see unchanged.
func NewLocalServer(handler http.Handler, apiToken string) *Server {
	client := NewRESTClient(localBaseURL, apiToken)
	client.Client = &http.Client{
		Timeout:   30 * time.Second,
		Transport: localRoundTripper{handler: handler},
	}
	return &Server{client: client}
}

type localRoundTripper struct {
	handler http.Handler
}

func (l localRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		defer req.Body.Close()
	}
	recorder := &responseRecorder{header: http.Header{}, status: http.StatusOK}
	l.handler.ServeHTTP(recorder, req)
	return &http.Response{
		Status:        http.StatusText(recorder.status),
		StatusCode:    recorder.status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        recorder.header,
		Body:          io.NopCloser(bytes.NewReader(recorder.body.Bytes())),
		ContentLength: int64(recorder.body.Len()),
		Request:       req,
	}, nil
}

// responseRecorder collects a handler's response in memory. Flush is a no-op
// rather than absent because gin's writer forwards it to the wrapped writer,
// and a handler that streams would otherwise panic here.
type responseRecorder struct {
	header      http.Header
	body        bytes.Buffer
	status      int
	wroteHeader bool
}

func (r *responseRecorder) Header() http.Header { return r.header }

func (r *responseRecorder) Write(p []byte) (int, error) {
	r.WriteHeader(http.StatusOK)
	return r.body.Write(p)
}

func (r *responseRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
}

func (r *responseRecorder) Flush() {}
