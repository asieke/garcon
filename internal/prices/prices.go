// Package prices serves model list prices to the dashboard from OpenRouter's
// public model catalogue, so the cost estimates track published rates without
// anyone typing numbers in.
//
// The catalogue is public and needs no key: GET https://openrouter.ai/api/v1/models
// lists every model OpenRouter routes with USD-per-token prices for prompt,
// completion and (where the vendor charges for it) cache reads and writes. The
// Anthropic and OpenAI entries mirror those vendors' own list prices, which is
// what the dashboard estimates against.
//
// The fetch happens on demand, the first time the dashboard asks, and then at
// most once a day; a proxy nobody looks at never calls out. The last good
// catalogue is kept on disk so a restart or an offline machine still has prices.
package prices

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"garcon/internal/local"
)

// DefaultURL is OpenRouter's public model list.
const DefaultURL = "https://openrouter.ai/api/v1/models"

// maxAge is how old a catalogue may be before a dashboard request triggers a refresh.
const maxAge = 24 * time.Hour

// Price is USD per one million tokens. Zero means the vendor does not charge for
// that kind of token (OpenAI cache writes); a model without a price is absent.
type Price struct {
	Input      float64 `json:"input"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	Output     float64 `json:"output"`
}

// Catalog is what GET /api/prices returns and what the cache file holds.
type Catalog struct {
	Source    string           `json:"source"`
	FetchedAt int64            `json:"fetched_at"` // unix ms of the last successful fetch, 0 = never
	Error     string           `json:"error,omitempty"`
	ErrorAt   int64            `json:"error_at,omitempty"`
	Models    map[string]Price `json:"models"` // OpenRouter id ("anthropic/claude-sonnet-5") to price
}

// Service fetches, caches and serves the catalogue.
type Service struct {
	URL    string
	path   string
	client *http.Client
	now    func() time.Time

	mu         sync.Mutex // guards cat and refreshing; never held across the fetch
	cat        Catalog
	refreshing bool
}

// New loads the cached catalogue from dir, if any. Nothing is fetched until asked.
func New(dir string) *Service {
	s := &Service{URL: DefaultURL, path: filepath.Join(dir, "prices.json"),
		client: &http.Client{Timeout: 30 * time.Second}, now: time.Now}
	s.cat = Catalog{Source: DefaultURL, Models: map[string]Price{}}
	if data, err := os.ReadFile(s.path); err == nil {
		var cached Catalog
		if json.Unmarshal(data, &cached) == nil && cached.Models != nil {
			s.cat = cached
		}
	}
	return s
}

// Catalog returns a copy of the current catalogue.
func (s *Service) Catalog() Catalog {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cat
	c.Models = make(map[string]Price, len(s.cat.Models))
	for k, v := range s.cat.Models {
		c.Models[k] = v
	}
	return c
}

// ServeHTTP: GET returns the catalogue, fetching it first if there is none and
// refreshing it in the background if it is a day old; POST refreshes it now.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		s.mu.Lock()
		never := s.cat.FetchedAt == 0
		stale := !never && s.now().Sub(time.UnixMilli(s.cat.FetchedAt)) > maxAge
		s.mu.Unlock()
		if never {
			s.Refresh()
		} else if stale {
			go s.Refresh()
		}
	case http.MethodPost:
		// Guard already limits the server to loopback, but a write is refused on a
		// stray Host anyway, as the settings endpoint does.
		if !local.Host(r.Host) {
			http.Error(w, "refreshing prices requires a loopback Host", http.StatusForbidden)
			return
		}
		s.Refresh()
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.Catalog())
}

// Refresh fetches the catalogue once; concurrent callers wait for the one in flight.
func (s *Service) Refresh() {
	s.mu.Lock()
	if s.refreshing {
		// Another caller is fetching; wait for it by taking the lock after it finishes.
		s.mu.Unlock()
		for {
			time.Sleep(50 * time.Millisecond)
			s.mu.Lock()
			done := !s.refreshing
			s.mu.Unlock()
			if done {
				return
			}
		}
	}
	s.refreshing = true
	s.mu.Unlock()

	models, err := s.fetch()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshing = false
	s.cat.Source = s.URL
	if err != nil {
		s.cat.Error, s.cat.ErrorAt = err.Error(), s.now().UnixMilli()
		log.Printf("prices: %v", err)
		return
	}
	s.cat.Models, s.cat.FetchedAt, s.cat.Error, s.cat.ErrorAt = models, s.now().UnixMilli(), "", 0
	if err := writeJSON(s.path, s.cat); err != nil {
		log.Printf("prices: could not cache to %s: %v", s.path, err)
	}
}

// fetch downloads and parses the catalogue.
func (s *Service) fetch() (map[string]Price, error) {
	req, err := http.NewRequest(http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", s.URL, res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	return Parse(body)
}

// Parse turns OpenRouter's model list into prices per million tokens. Variants
// such as ":batch", ":free" or ":thinking" are separate products and skipped,
// and so is anything priced dynamically (negative) or not at all.
func Parse(body []byte) (map[string]Price, error) {
	var doc struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
				CacheRead  string `json:"input_cache_read"`
				CacheWrite string `json:"input_cache_write"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("model list is not the expected JSON: %w", err)
	}
	if len(doc.Data) == 0 {
		return nil, errors.New("model list is empty")
	}
	out := make(map[string]Price, len(doc.Data))
	for _, m := range doc.Data {
		if m.ID == "" || strings.Contains(m.ID, ":") {
			continue
		}
		in, okIn := perMillion(m.Pricing.Prompt)
		outp, okOut := perMillion(m.Pricing.Completion)
		if !okIn || !okOut {
			continue
		}
		p := Price{Input: in, Output: outp}
		// A missing cache price means the vendor bills those tokens at the plain input
		// rate; the dashboard would rather charge full price than pretend they are free.
		if v, ok := perMillion(m.Pricing.CacheRead); ok {
			p.CacheRead = v
		} else {
			p.CacheRead = in
		}
		if v, ok := perMillion(m.Pricing.CacheWrite); ok {
			p.CacheWrite = v
		} else {
			p.CacheWrite = in
		}
		out[m.ID] = p
	}
	if len(out) == 0 {
		return nil, errors.New("model list had no usable prices")
	}
	return out, nil
}

// perMillion converts OpenRouter's USD-per-token string to USD per million tokens.
func perMillion(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	// Rounded to a millionth of a dollar so 0.0000002 per token reads as 0.2, not 0.19999999999999998.
	return math.Round(v*1_000_000*1_000_000) / 1_000_000, true
}

// writeJSON writes atomically; the file holds nothing secret but shares the data
// directory's permissions.
func writeJSON(path string, v any) error {
	os.MkdirAll(filepath.Dir(path), 0o700)
	data, _ := json.Marshal(v)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
