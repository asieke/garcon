package prices

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

const sample = `{"data":[
 {"id":"anthropic/claude-sonnet-5","pricing":{"prompt":"0.000002","completion":"0.00001","input_cache_read":"0.0000002","input_cache_write":"0.0000025"}},
 {"id":"anthropic/claude-sonnet-5:batch","pricing":{"prompt":"0.000001","completion":"0.000005"}},
 {"id":"openai/gpt-5.1","pricing":{"prompt":"0.00000125","completion":"0.00001","input_cache_read":"0.000000125"}},
 {"id":"openrouter/auto","pricing":{"prompt":"-1","completion":"-1"}},
 {"id":"vendor/free","pricing":{"prompt":"0","completion":"0"}}
]}`

func TestParse(t *testing.T) {
	got, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 priced models (variants and dynamic prices skipped), got %d: %v", len(got), got)
	}
	if p := got["anthropic/claude-sonnet-5"]; p != (Price{Input: 2, CacheRead: 0.2, CacheWrite: 2.5, Output: 10}) {
		t.Errorf("sonnet: %+v", p)
	}
	// OpenAI lists no cache-write price: those tokens cost plain input, not nothing.
	if p := got["openai/gpt-5.1"]; p.CacheWrite != 1.25 || p.CacheRead != 0.125 {
		t.Errorf("gpt-5.1: %+v", p)
	}
	if p := got["vendor/free"]; p != (Price{}) {
		t.Errorf("free model should be priced at zero, got %+v", p)
	}
	for _, bad := range []string{`not json`, `{"data":[]}`, `{"data":[{"id":"x","pricing":{"prompt":"-1","completion":"1"}}]}`} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestServeFetchesOnceCachesAndRefreshes(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(sample))
	}))
	defer srv.Close()
	dir := t.TempDir()
	s := New(dir)
	s.URL = srv.URL
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	get := func() Catalog {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/prices", nil))
		if w.Code != 200 {
			t.Fatalf("GET: %d %s", w.Code, w.Body)
		}
		var c Catalog
		if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	c := get()
	if c.FetchedAt != now.UnixMilli() || len(c.Models) != 3 || c.Source != srv.URL || c.Error != "" {
		t.Fatalf("first GET should fetch: %+v", c)
	}
	get()
	if hits.Load() != 1 {
		t.Fatalf("a fresh catalogue must not be fetched again, got %d fetches", hits.Load())
	}
	if _, err := os.Stat(filepath.Join(dir, "prices.json")); err != nil {
		t.Fatalf("catalogue not cached: %v", err)
	}

	// A new service in the same directory starts from the cache without a fetch.
	s2 := New(dir)
	s2.URL = srv.URL
	s2.now = s.now
	if c := s2.Catalog(); c.FetchedAt != now.UnixMilli() || len(c.Models) != 3 {
		t.Fatalf("cache not loaded: %+v", c)
	}
	if hits.Load() != 1 {
		t.Fatalf("loading the cache must not fetch, got %d fetches", hits.Load())
	}

	// POST refreshes now, and requires a loopback Host.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/prices", nil)
	req.Host = "evil.example"
	s.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || hits.Load() != 1 {
		t.Fatalf("POST from a foreign Host: %d, %d fetches", w.Code, hits.Load())
	}
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/prices", nil)
	req.Host = "127.0.0.1:4141"
	s.ServeHTTP(w, req)
	if w.Code != 200 || hits.Load() != 2 {
		t.Fatalf("POST refresh: %d, %d fetches", w.Code, hits.Load())
	}

	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/prices", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE: %d", w.Code)
	}
}

func TestFetchFailureKeepsLastCatalogue(t *testing.T) {
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		w.Write([]byte(sample))
	}))
	defer srv.Close()
	s := New(t.TempDir())
	s.URL = srv.URL
	s.Refresh()
	fail.Store(true)
	s.Refresh()
	c := s.Catalog()
	if len(c.Models) != 3 || c.FetchedAt == 0 {
		t.Fatalf("failed refresh must keep the last catalogue: %+v", c)
	}
	if c.Error == "" || c.ErrorAt == 0 {
		t.Fatalf("failed refresh must be reported: %+v", c)
	}
	fail.Store(false)
	s.Refresh()
	if c := s.Catalog(); c.Error != "" || c.ErrorAt != 0 {
		t.Fatalf("a good refresh clears the error: %+v", c)
	}
}
