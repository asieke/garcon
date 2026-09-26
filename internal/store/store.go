// Package store exposes the local SQLite request ledger.
package store

import (
	"encoding/json"
	"garcon/internal/accounts"
	"garcon/internal/codexmetadata"
	"garcon/internal/database"
	"garcon/internal/usage"
	"log"
	"path/filepath"
	"sync"
)

type Store struct {
	DB       *database.DB
	Tasks    *codexmetadata.Reader
	mu       sync.Mutex
	recent   []RecentRecord
	baseline int
}

func Open(path string) (*Store, error) {
	db, err := database.Open(path)
	if err != nil {
		return nil, err
	}
	s := &Store{DB: db, recent: []RecentRecord{}}
	s.baseline = s.Len()
	return s, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func (s *Store) Path() string { return s.DB.Path }
func (s *Store) Dir() string  { return filepath.Dir(s.DB.Path) }
func (s *Store) Save(r usage.Record) {
	if err := s.SaveRecord(r); err != nil {
		log.Printf("Could not persist request: %v", err)
	}
}
func (s *Store) SaveRecord(r usage.Record) error {
	if r.Kind == "" {
		r.Kind = "completion"
	}
	if r.State == "" {
		r.State = "complete"
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var id any
	if r.RequestID != "" {
		id = r.RequestID
	}
	var seq int
	err = s.DB.QueryRow(`INSERT INTO requests(request_id,time,account_id,session_id,model,status,kind,state,record) VALUES(?,?,?,?,?,?,?,?,?)
 ON CONFLICT(request_id) DO UPDATE SET model=excluded.model,status=excluded.status,state=excluded.state,record=excluded.record
 RETURNING sequence`, id, r.Time, r.AccountID, r.SessionID, r.Model, r.Status, r.Kind, r.State, string(b)).Scan(&seq)
	if err != nil {
		return err
	}
	found := false
	for i := range s.recent {
		if s.recent[i].Sequence == seq {
			s.recent[i].Record = r
			found = true
			break
		}
	}
	if !found {
		s.recent = append(s.recent, RecentRecord{seq, r})
		if len(s.recent) > 30 {
			s.recent = s.recent[len(s.recent)-30:]
		}
	}
	return nil
}
func (s *Store) Len() int {
	var n int
	s.DB.QueryRow("SELECT count(*) FROM requests").Scan(&n)
	return n
}
func (s *Store) All() []usage.Record {
	out := []usage.Record{}
	rows, err := s.DB.Query("SELECT record FROM requests WHERE kind='completion' AND state!='streaming' ORDER BY sequence")
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		var r usage.Record
		if rows.Scan(&b) == nil && json.Unmarshal([]byte(b), &r) == nil {
			r.Account = accounts.DisplayLabel(r.Account)
			out = append(out, r)
		}
	}
	return out
}

type RecentRecord struct {
	Sequence int `json:"sequence"`
	usage.Record
}
type RequestFeed struct {
	Requests      []RecentRecord `json:"requests"`
	TotalRequests int            `json:"total_requests"`
}

func (s *Store) Recent() []RecentRecord { return s.RequestFeed().Requests }
func (s *Store) RequestFeed() RequestFeed {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := append([]RecentRecord{}, s.recent...)
	for i := range rows {
		rows[i].Account = accounts.DisplayLabel(rows[i].Account)
	}
	return RequestFeed{rows, s.Len()}
}
