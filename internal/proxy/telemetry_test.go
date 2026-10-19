package proxy

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"garcon/internal/telemetry"
	"garcon/internal/usage"
)

func TestTelemetryBlocksRoutedProviderBeforeRecording(t *testing.T) {
	policy, err := telemetry.New(telemetry.Config{Enabled: true, BlockHosts: []string{"api.anthropic.com"}})
	if err != nil {
		t.Fatal(err)
	}
	p := &Proxy{Telemetry: policy, Start: func(usage.Record) { t.Fatal("blocked request recorded as provider traffic") }}
	u, _ := url.Parse("https://api.anthropic.com")
	r := httptest.NewRequest("POST", "/claude/v1/messages?secret=not-logged", strings.NewReader("private prompt"))
	w := httptest.NewRecorder()
	p.Serve(w, r, Route{Harness: "claude", Provider: "anthropic", Rest: "v1/messages", Target: u})
	if w.Code != 403 || w.Header().Get("X-Garcon-Telemetry-Blocked") != "true" {
		t.Fatalf("block: %d %s", w.Code, w.Body.String())
	}
}
