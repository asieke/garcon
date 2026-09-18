// Package store keeps this machine's usage log (append-only usage.jsonl) and the
// cache of rows pulled from other machines (remote.jsonl) in memory and on disk.
package store

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"

	"garcon/internal/accounts"
	"garcon/internal/usage"
)

// Store is safe for concurrent use. Local rows carry the device label in memory
// only; remote rows carry id, device id and label on disk too.
type Store struct {
	mu             sync.Mutex
	path           string
	device         string
	records        []usage.Record
	remote         []usage.Record
	seen           map[string]struct{}
	logFile        *os.File
	remoteFile     *os.File
	recent         []RecentRecord
	recentSequence int
	// OnSave, if set, is called (outside the lock) after every Save.
	OnSave func()
}

// Open loads usage.jsonl at path and remote.jsonl beside it, creating both if needed.
func Open(path string) (*Store, error) {
	s := &Store{path: path, records: []usage.Record{}, remote: []usage.Record{}, seen: map[string]struct{}{}}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s.records = append(s.records, readRecords(path)...)
	for _, rec := range readRecords(s.RemotePath()) {
		if _, dup := s.seen[rec.ID]; rec.ID == "" || dup {
			continue
		}
		s.seen[rec.ID] = struct{}{}
		s.remote = append(s.remote, rec)
	}
	// Loaded history establishes the sequence baseline, but isn't live traffic.
	s.recentSequence = len(s.records) + len(s.remote)
	var err error
	if s.logFile, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err != nil {
		return nil, err
	}
	if s.remoteFile, err = os.OpenFile(s.RemotePath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err != nil {
		return nil, err
	}
	return s, nil
}

func readRecords(path string) []usage.Record {
	var out []usage.Record
	data, _ := os.ReadFile(path)
	for _, line := range bytes.Split(data, []byte("\n")) {
		var rec usage.Record
		if json.Unmarshal(line, &rec) == nil {
			out = append(out, rec)
		}
	}
	return out
}

// Path is the usage log; Dir holds it and the sync files; RemotePath is the remote cache.
func (s *Store) Path() string       { return s.path }
func (s *Store) Dir() string        { return filepath.Dir(s.path) }
func (s *Store) RemotePath() string { return filepath.Join(s.Dir(), "remote.jsonl") }

// Save appends one local record to the log and to memory. The line is written
// without the device label so the on-disk format never changes.
func (s *Store) Save(rec usage.Record) {
	line, _ := json.Marshal(rec)
	s.mu.Lock()
	rec.Device = s.device
	s.records = append(s.records, rec)
	s.appendRecent(rec, false)
	if _, err := s.logFile.Write(append(line, '\n')); err != nil {
		log.Print(err)
	}
	s.mu.Unlock()
	if s.OnSave != nil {
		s.OnSave()
	}
}

// SetDevice relabels every local row with this machine's display name.
func (s *Store) SetDevice(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.device = name
	for i := range s.records {
		s.records[i].Device = name
	}
	for i := range s.recent {
		if !s.recent[i].Remote {
			s.recent[i].Device = name
		}
	}
}

// Len is the number of local records.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

// Batch copies local records [start, start+n), clipped to what exists.
func (s *Store) Batch(start, n int) []usage.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	end := min(start+n, len(s.records))
	if start >= end {
		return nil
	}
	return append([]usage.Record(nil), s.records[start:end]...)
}

// All returns local rows followed by remote rows, as one fresh slice.
func (s *Store) All() []usage.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := make([]usage.Record, 0, len(s.records)+len(s.remote))
	all = append(append(all, s.records...), s.remote...)
	for i := range all {
		all[i].Account = accounts.DisplayLabel(all[i].Account)
	}
	return all
}

// RecentRecord orders local and synced records by arrival, not request timestamp.
type RecentRecord struct {
	Sequence int  `json:"sequence"`
	Remote   bool `json:"remote"`
	usage.Record
}

// appendRecent is called under mu only for new local or deduplicated sync arrivals.
func (s *Store) appendRecent(rec usage.Record, remote bool) {
	s.recentSequence++
	s.recent = append(s.recent, RecentRecord{Sequence: s.recentSequence, Remote: remote, Record: rec})
	if len(s.recent) > 30 {
		s.recent = s.recent[len(s.recent)-30:]
	}
}

// Recent returns at most 30 new arrivals from this server run, oldest first.
// Historical rows loaded from disk are excluded, as are duplicate sync rows.
func (s *Store) Recent() []RecentRecord {
	return s.RequestFeed().Requests
}

type RequestFeed struct {
	Requests      []RecentRecord `json:"requests"`
	TotalRequests int            `json:"total_requests"`
}

// RequestFeed takes the live arrivals and all-machine count under the same lock.
func (s *Store) RequestFeed() RequestFeed {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := append([]RecentRecord{}, s.recent...)
	for i := range rows {
		rows[i].Account = accounts.DisplayLabel(rows[i].Account)
	}
	return RequestFeed{Requests: rows, TotalRequests: len(s.records) + len(s.remote)}
}

// Remote returns a copy of the rows pulled from other machines.
func (s *Store) Remote() []usage.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]usage.Record(nil), s.remote...)
}

// AddRemote caches one row from another machine. It reports false for a
// duplicate id (or a row without one), which is not written.
func (s *Store) AddRemote(rec usage.Record) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.seen[rec.ID]; dup || rec.ID == "" {
		return false
	}
	s.seen[rec.ID] = struct{}{}
	s.remote = append(s.remote, rec)
	s.appendRecent(rec, true)
	line, _ := json.Marshal(rec)
	if _, err := s.remoteFile.Write(append(line, '\n')); err != nil {
		log.Print(err)
	}
	return true
}

// ClearRemote forgets every cached remote row, in memory and on disk.
func (s *Store) ClearRemote() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.remote, s.seen = []usage.Record{}, map[string]struct{}{}
	local := s.recent[:0]
	for _, row := range s.recent {
		if !row.Remote {
			local = append(local, row)
		}
	}
	s.recent = local
	s.remoteFile.Truncate(0)
}
