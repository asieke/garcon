// Package store keeps this machine's append-only usage log in memory and on disk.
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

// Store is safe for concurrent use.
type Store struct {
	mu             sync.Mutex
	path           string
	records        []usage.Record
	logFile        *os.File
	recent         []RecentRecord
	recentSequence int
}

// Open loads the local usage log, creating it if needed. Legacy remote caches are ignored.
func Open(path string) (*Store, error) {
	s := &Store{path: path, records: []usage.Record{}}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s.records = append(s.records, readRecords(path)...)
	// Loaded history establishes the sequence baseline, but isn't live traffic.
	s.recentSequence = len(s.records)
	var err error
	if s.logFile, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err != nil {
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

// Path is the usage log; Dir holds the local data files.
func (s *Store) Path() string { return s.path }
func (s *Store) Dir() string  { return filepath.Dir(s.path) }

// Save appends one local record to the log and to memory.
func (s *Store) Save(rec usage.Record) {
	line, _ := json.Marshal(rec)
	s.mu.Lock()
	s.records = append(s.records, rec)
	s.appendRecent(rec)
	if _, err := s.logFile.Write(append(line, '\n')); err != nil {
		log.Print(err)
	}
	s.mu.Unlock()
}

// Len is the number of local records.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

// All returns a fresh slice of local rows.
func (s *Store) All() []usage.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := append([]usage.Record{}, s.records...)
	for i := range all {
		all[i].Account = accounts.DisplayLabel(all[i].Account)
	}
	return all
}

// RecentRecord orders local records by arrival, not request timestamp.
type RecentRecord struct {
	Sequence int `json:"sequence"`
	usage.Record
}

// appendRecent is called under mu only for new local arrivals.
func (s *Store) appendRecent(rec usage.Record) {
	s.recentSequence++
	s.recent = append(s.recent, RecentRecord{Sequence: s.recentSequence, Record: rec})
	if len(s.recent) > 30 {
		s.recent = s.recent[len(s.recent)-30:]
	}
}

// Recent returns at most 30 new arrivals from this server run, oldest first.
// Historical rows loaded from disk are excluded.
func (s *Store) Recent() []RecentRecord {
	return s.RequestFeed().Requests
}

type RequestFeed struct {
	Requests      []RecentRecord `json:"requests"`
	TotalRequests int            `json:"total_requests"`
}

// RequestFeed takes the live arrivals and local count under the same lock.
func (s *Store) RequestFeed() RequestFeed {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := append([]RecentRecord{}, s.recent...)
	for i := range rows {
		rows[i].Account = accounts.DisplayLabel(rows[i].Account)
	}
	return RequestFeed{Requests: rows, TotalRequests: len(s.records)}
}
