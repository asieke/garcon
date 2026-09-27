package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"garcon/internal/usage"
)

// Simulate a body that reports a read/close error after returning its payload.
type endingBody struct {
	io.Reader
	readError, closeError error
	cancel                context.CancelFunc
}

func (b *endingBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if err == io.EOF {
		if b.cancel != nil {
			b.cancel()
		}
		if b.readError != nil {
			err = b.readError
		}
	}
	return n, err
}

func (b *endingBody) Close() error { return b.closeError }

func TestResponseCompletionSurvivesStreamTeardown(t *testing.T) {
	const completed = "data: {\"type\":\"response.completed\",\"response\":{\"model\":\"test\",\"usage\":{\"input_tokens\":12,\"output_tokens\":7}}}\n\n"
	const partial = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"response.completed\"}\n\n"
	for _, tc := range []struct {
		name, body, state, diagnostic string
		status                        int
		cancel                        bool
		readError, closeError         error
	}{
		{name: "normal completion", body: completed, state: "complete"},
		{name: "cancel after completion", body: completed, cancel: true, readError: context.Canceled, state: "complete"},
		{name: "cancel after final event without newline", body: strings.TrimSpace(completed), cancel: true, readError: context.Canceled, state: "complete"},
		{name: "trailing read error", body: completed, readError: io.ErrUnexpectedEOF, state: "complete"},
		{name: "trailing close error", body: completed, closeError: context.Canceled, state: "complete"},
		{name: "client cancels before completion", body: partial, cancel: true, readError: context.Canceled, state: "interrupted", diagnostic: "Client disconnected or canceled the request"},
		{name: "upstream truncation before completion", body: partial, readError: io.ErrUnexpectedEOF, state: "interrupted", diagnostic: "Upstream response stream ended unexpectedly"},
		{name: "output item done is not response complete", body: "data: {\"type\":\"response.output_item.done\"}\n\n", readError: io.ErrUnexpectedEOF, state: "interrupted", diagnostic: "Upstream response stream ended unexpectedly"},
		{name: "usage alone is not completion", body: "data: {\"usage\":{\"input_tokens\":12,\"output_tokens\":7}}\n\n", readError: io.ErrUnexpectedEOF, state: "interrupted", diagnostic: "Upstream response stream ended unexpectedly"},
		{name: "provider failure survives cancellation", body: "data: {\"type\":\"response.failed\"}\n\n", cancel: true, readError: context.Canceled, state: "failed", diagnostic: "Upstream reported a failed or incomplete response"},
		{name: "incomplete survives teardown", body: "data: {\"type\":\"response.incomplete\"}\n\n", readError: io.ErrUnexpectedEOF, state: "failed", diagnostic: "Upstream reported a failed or incomplete response"},
		{name: "failure is not hidden by completion", body: completed + "data: {\"type\":\"error\"}\n\n", readError: io.ErrUnexpectedEOF, state: "failed", diagnostic: "Upstream reported a failed or incomplete response"},
		{name: "HTTP failure survives cancellation", body: "unavailable", status: 503, cancel: true, readError: context.Canceled, state: "failed", diagnostic: "Service Unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := &endingBody{Reader: strings.NewReader(tc.body), readError: tc.readError, closeError: tc.closeError}
			if tc.cancel {
				body.cancel = cancel
			}
			status := tc.status
			if status == 0 {
				status = http.StatusOK
			}
			original := http.DefaultTransport
			http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}, nil
			})
			defer func() { http.DefaultTransport = original }()
			var records []usage.Record
			p := Proxy{Save: func(r usage.Record) { records = append(records, r) }}
			const path = "/codex/backend-api/codex/responses"
			rt, _ := ParseRoute(path)
			r := httptest.NewRequest("POST", path, strings.NewReader(`{"model":"test"}`)).WithContext(ctx)
			w := httptest.NewRecorder()
			p.Serve(w, r, rt)
			if len(records) != 1 {
				t.Fatalf("got %d final records, want one", len(records))
			}
			got := records[0]
			if got.State != tc.state || got.Error != tc.diagnostic || got.Status != status {
				t.Fatalf("incorrect classification: %+v", got)
			}
			if strings.HasPrefix(tc.body, strings.TrimSpace(completed)) && (got.Input != 12 || got.Output != 7) {
				t.Fatalf("lost usage: %+v", got)
			}
			if w.Body.String() != tc.body || w.Code != status {
				t.Fatalf("changed forwarded response: %d %q", w.Code, w.Body.String())
			}
		})
	}
}

func TestCompletionEventAcrossReadBoundaries(t *testing.T) {
	const body = "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"output_tokens\":7}}}\r\n\r\n"
	var completed bool
	var got usage.Record
	stream := &tap{ReadCloser: io.NopCloser(strings.NewReader(body)), done: func(r usage.Record, failure string, complete bool, err error) {
		got, completed = r, complete
	}}
	var forwarded strings.Builder
	buffer := make([]byte, 1)
	for {
		n, err := stream.Read(buffer)
		forwarded.Write(buffer[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	stream.Close()
	if !completed || got.Output != 7 || forwarded.String() != body {
		t.Fatalf("lost split event: completed=%v record=%+v", completed, got)
	}
}
