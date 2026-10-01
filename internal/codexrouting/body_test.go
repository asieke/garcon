package codexrouting

import (
	"crypto/sha256"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestImageBearingResponseExceedsOldLimitAndKeepsRouting(t *testing.T) {
	r, _, _ := setup(t)
	if err := r.Pin("a"); err != nil {
		t.Fatal(err)
	}
	// Put routing metadata after the large image to cover the entire JSON body.
	payload := `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,` + strings.Repeat("A", 34<<20) + `"}]}],"model":"model","client_metadata":{"session_id":"image-conversation"}}`
	wantHash := sha256.Sum256([]byte(payload))
	for _, chunked := range []bool{false, true} {
		req := request("")
		req.Body = io.NopCloser(strings.NewReader(payload))
		req.ContentLength = int64(len(payload))
		if chunked {
			req.ContentLength = -1
		}
		release, err := r.Prepare(req)
		if err != nil {
			t.Fatal(err)
		}
		release()
		if req.Header.Get("ChatGPT-Account-Id") != "a" || Details(req).Session != "image-conversation" || Details(req).Model != "model" {
			t.Fatalf("image conversation lost routing: %+v", Details(req))
		}
		h := sha256.New()
		n, err := io.Copy(h, req.Body)
		req.Body.Close()
		if err != nil || n != int64(len(payload)) || string(h.Sum(nil)) != string(wantHash[:]) {
			t.Fatal("large image request bytes changed", err)
		}
	}
}

type bodyReadFailure struct{}

func (bodyReadFailure) Read([]byte) (int, error) { return 0, errors.New("upload interrupted") }
func (bodyReadFailure) Close() error             { return nil }

func TestRoutingBodyBoundariesAndReadFailure(t *testing.T) {
	// Exercise the same bounded reader with a small limit to avoid allocating
	// hundreds of MiB just to test an over-limit or interrupted upload.
	const limit = 1 << 20
	for _, tc := range []struct {
		name   string
		size   int
		length int64
		status int
	}{
		{"at limit", limit, limit, 0},
		{"chunked at limit", limit, -1, 0},
		{"known oversized", 0, limit + 1, 413},
		{"chunked oversized", limit + 1, -1, 413},
		{"underreported oversized", limit + 1, 1, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := request("body-test")
			req.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", tc.size)))
			req.ContentLength = tc.length
			b, err := readRoutingBody(req, limit)
			if tc.status == 0 {
				if err != nil || len(b) != tc.size {
					t.Fatalf("valid body rejected: %v", err)
				}
				req.Body.Close()
			} else if err == nil || ErrorStatus(err) != tc.status {
				t.Fatalf("wrong size error: %v", err)
			}
		})
	}
	req := request("interrupted-upload")
	req.Body = bodyReadFailure{}
	_, err := readRoutingBody(req, limit)
	if err == nil || ErrorStatus(err) != 400 || strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("read failure misreported as size limit: %v", err)
	}
}
