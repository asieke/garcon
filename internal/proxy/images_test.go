package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"garcon/internal/usage"
)

func TestCodexImagesPreserveCallerAndPayloadWithoutSession(t *testing.T) {
	router := emptyCodexPool(t)
	for _, tc := range []struct {
		name, endpoint, contentType, body string
	}{
		{"generation", "generations", "application/json", `{"prompt":"test","size":"1024x1024","transparent_background":true}`},
		{"json edit", "edits", "application/json", `{"model":"image-model","prompt":"test","images":[{"image_url":"data:image/png;base64,AAAA"}],"extra_image_parameter":true}`},
		{"multipart edit", "edits", "multipart/form-data; boundary=image-boundary", "--image-boundary\r\nContent-Disposition: form-data; name=\"image\"; filename=\"input.png\"\r\nContent-Type: image/png\r\n\r\n\x89PNG\x00\xff\r\n--image-boundary--\r\n"},
		// The coding router's former 32 MiB limit must not reject image uploads.
		{"large image", "edits", "application/json", `{"image":"` + strings.Repeat("A", 33<<20) + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := "/codex/backend-api/codex/images/" + tc.endpoint
			const query = "image_option=unchanged"
			const response = `{"data":[{"b64_json":"unchanged-image-output"}]}`
			calls := 0
			status := http.StatusOK
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != tc.body || r.URL.Path != "/backend-api/codex/images/"+tc.endpoint || r.URL.RawQuery != query {
					t.Error("image payload or URL changed", err)
				}
				if r.Header.Get("Authorization") != "Bearer image-caller" || r.Header.Get("ChatGPT-Account-Id") != "image-account" || r.Header.Get("Content-Type") != tc.contentType {
					t.Error("image caller or content type changed")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				io.WriteString(w, response)
			}))
			defer upstream.Close()
			target, _ := url.Parse(upstream.URL)
			var saved usage.Record
			p := Proxy{Codex: router, Save: func(r usage.Record) { saved = r }}
			rt, _ := ParseRoute(path)
			rt.Target = target
			for _, code := range []int{http.StatusOK, http.StatusTooManyRequests} {
				status = code
				r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4141"+path+"?"+query, strings.NewReader(tc.body))
				r.RemoteAddr = "127.0.0.1:1234"
				r.Header.Set("Authorization", "Bearer image-caller")
				r.Header.Set("ChatGPT-Account-Id", "image-account")
				r.Header.Set("Content-Type", tc.contentType)
				w := httptest.NewRecorder()
				p.Serve(w, r, rt)
				if w.Code != code || w.Body.String() != response || w.Header().Get("X-Garcon-Account-Id") != "" {
					t.Fatalf("image response changed: %d %s", w.Code, w.Body.String())
				}
				state := "complete"
				if code >= 400 {
					state = "failed"
				}
				if saved.AccountID != "image-account" || saved.SessionID != "" || saved.State != state || saved.Status != code || saved.Kind != "request" {
					t.Fatalf("incorrect image attribution: %+v", saved)
				}
			}
			if calls != 2 {
				t.Fatalf("image request replayed: %d upstream calls", calls)
			}
		})
	}
}

func TestImageExemptionKeepsCodingSessionRequirement(t *testing.T) {
	p := Proxy{Codex: emptyCodexPool(t)}
	for _, path := range []string{
		"/codex/backend-api/codex/responses",
		"/codex/backend-api/codex/images/edits-lookalike",
		"/codex/backend-api/codex/images/generations/responses",
	} {
		rt, _ := ParseRoute(path)
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4141"+path, strings.NewReader(`{"model":"coding-model"}`))
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		p.Serve(w, r, rt)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "stable Codex session identifier") {
			t.Fatalf("coding session guard lost on %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/other/chatgpt/backend-api/codex/images/edits", "/other/openai/backend-api/codex/images/generations"} {
		rt, _ := ParseRoute(path)
		if isCodexImage(rt) {
			t.Fatalf("image exemption matched a different client/provider: %s", path)
		}
	}
}
