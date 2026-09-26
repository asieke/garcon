package proxy

import (
	"bytes"
	"encoding/json"
	"garcon/internal/usage"
	"io"
	"sync"
)

// tap folds usage as SSE lines arrive. Memory stays bounded even for a long
// conversation; no prompt or response content is retained in the request ledger.
type tap struct {
	io.ReadCloser
	pending   []byte
	dropping  bool
	rec       usage.Record
	failure   string
	readError error
	once      sync.Once
	done      func(usage.Record, string, error)
}

const maxEventBytes = 16 << 20

func (t *tap) fold(line []byte) {
	usage.Fold(line, &t.rec)
	// Save a fixed diagnostic, never a provider message that might echo a prompt.

	var event struct {
		Type string `json:"type"`
	}
	raw := bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(line), []byte("data:")))
	if json.Unmarshal(raw, &event) == nil && (event.Type == "response.failed" || event.Type == "response.incomplete" || event.Type == "error") {
		t.failure = "Upstream reported a failed or incomplete response"
	}

}
func (t *tap) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	data := p[:n]
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		part := data
		if i >= 0 {
			part = data[:i]
		}
		if !t.dropping {
			if len(t.pending)+len(part) > maxEventBytes {
				t.pending = nil
				t.dropping = true
			} else {
				t.pending = append(t.pending, part...)
			}
		}
		if i < 0 {
			break
		}
		if !t.dropping {
			t.fold(t.pending)
		}
		t.pending = t.pending[:0]
		t.dropping = false
		data = data[i+1:]
	}
	if err != nil && err != io.EOF {
		t.readError = err
	}
	return n, err
}
func (t *tap) Close() error {
	err := t.ReadCloser.Close()
	t.once.Do(func() {
		if len(t.pending) > 0 && !t.dropping {
			t.fold(t.pending)
		}
		if t.readError == nil {
			t.readError = err
		}
		t.done(t.rec, t.failure, t.readError)
		t.pending = nil
	})
	return err
}
