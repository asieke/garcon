package proxy

import (
	"garcon/internal/usage"
	"io"
	"strings"
	"testing"
)

func TestTapFoldsIncrementallyAndReportsStreamFailure(t *testing.T) {
	body := `data: {"type":"message_start","message":{"model":"model","usage":{"input_tokens":12}}}` + "\n" + strings.Repeat("data: {}\n", 10000) + `data: {"type":"message_delta","usage":{"output_tokens":7}}` + "\n" + `data: {"type":"response.failed"}` + "\n"
	var got usage.Record
	var failure string
	calls := 0
	tap := &tap{ReadCloser: io.NopCloser(strings.NewReader(body)), done: func(r usage.Record, f string, e error) { got = r; failure = f; calls++ }}
	if _, err := io.Copy(io.Discard, tap); err != nil {
		t.Fatal(err)
	}
	tap.Close()
	tap.Close()
	if got.Input != 12 || got.Output != 7 || failure == "" || calls != 1 {
		t.Fatalf("lost streamed accounting: %+v %s calls=%d", got, failure, calls)
	}
	if len(tap.pending) != 0 {
		t.Fatal("retained response content")
	}
}

func TestTapDoesNotTreatGeneratedTextAsAnError(t *testing.T) {
	var failure string
	tap := &tap{ReadCloser: io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"response.failed\"}\n")), done: func(r usage.Record, f string, e error) { failure = f }}
	io.Copy(io.Discard, tap)
	tap.Close()
	if failure != "" {
		t.Fatal("generated text was interpreted as an error event")
	}
}
