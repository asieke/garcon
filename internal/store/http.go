package store

import (
	"encoding/json"
	"fmt"
	"garcon/internal/local"
	"garcon/internal/usage"
	"net/http"
	"strconv"
	"strings"
)

func (s *Store) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", 405)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var result any
	var err error
	switch r.URL.Path {
	case "/api/sessions":
		var sessions []map[string]any
		sessions, err = s.sessions()
		// Task titles and workspace paths are exposed only to local clients,
		// even if the operator enables the remote usage dashboard.
		if err == nil && s.Tasks != nil && local.Host(r.RemoteAddr) && local.Host(r.Host) {
			ids := []string{}
			for _, session := range sessions {
				if id := session["session_id"].(string); id != "" && session["harness"] == "codex" {
					ids = append(ids, id)
				}
			}
			tasks := s.Tasks.Lookup(r.Context(), ids)
			for _, session := range sessions {
				if task, ok := tasks[session["session_id"].(string)]; ok && session["harness"] == "codex" {
					session["task"] = task
				}
			}
		}
		result = sessions
	case "/api/logs":
		result, err = s.logs(r)
	case "/api/events":
		result, err = s.events(r)
	case "/api/analytics":
		result, err = s.analytics(r)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Could not read local database", 500)
		return
	}
	json.NewEncoder(w).Encode(result)
}
func page(r *http.Request) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return limit, max(0, offset)
}
func (s *Store) logs(r *http.Request) (any, error) {
	limit, offset := page(r)
	where := "1=1"
	args := []any{}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		where += ` AND (model LIKE ? OR account_id LIKE ? OR session_id LIKE ? OR json_extract(record,'$.account') LIKE ? OR json_extract(record,'$.path') LIKE ? OR request_id LIKE ? OR json_extract(record,'$.harness') LIKE ? OR json_extract(record,'$.provider') LIKE ?)`
		for i := 0; i < 8; i++ {
			args = append(args, "%"+q+"%")
		}
	}
	if value := r.URL.Query().Get("harness"); r.URL.Query().Has("harness") {
		where += ` AND coalesce(nullif(json_extract(record,'$.harness'),''),CASE WHEN EXISTS (SELECT 1 FROM sessions s WHERE s.session_id=requests.session_id AND s.session_id!='') THEN 'codex' ELSE '' END)=?`
		args = append(args, value)
	}
	if value := r.URL.Query().Get("session_id"); value != "" {
		where += " AND session_id=?"
		args = append(args, value)
	}
	if r.URL.Query().Get("errors") == "true" {
		where += " AND (status>=400 OR state IN ('failed','interrupted'))"
	}
	if r.URL.Query().Get("hide_zero_tokens") == "true" {
		where += ` AND (coalesce(json_extract(record,'$.input'),0)+coalesce(json_extract(record,'$.cache_read'),0)+coalesce(json_extract(record,'$.cache_write'),0)+coalesce(json_extract(record,'$.output'),0))>0`
	}
	var total int
	if err := s.DB.QueryRow("SELECT count(*) FROM requests WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query("SELECT sequence,record FROM requests WHERE "+where+" ORDER BY sequence DESC LIMIT ? OFFSET ?", append(args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecentRecord{}
	for rows.Next() {
		var rec RecentRecord
		var b string
		if err = rows.Scan(&rec.Sequence, &b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(b), &rec.Record); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return map[string]any{"requests": out, "total": total, "offset": offset, "limit": limit}, rows.Err()
}
func (s *Store) events(r *http.Request) (any, error) {
	var total int
	if err := s.DB.QueryRow("SELECT count(*) FROM events").Scan(&total); err != nil {
		return nil, err
	}
	limit, offset := page(r)
	rows, err := s.DB.Query("SELECT id,time,message FROM events ORDER BY id DESC LIMIT ? OFFSET ?", limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, t int64
		var msg string
		if err = rows.Scan(&id, &t, &msg); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "time": t, "message": msg})
	}
	return map[string]any{"events": out, "total": total}, rows.Err()
}
func (s *Store) analytics(r *http.Request) (any, error) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	rows, err := s.DB.Query(`SELECT model,coalesce(json_extract(record,'$.account'),''),time/3600000*3600000,
 count(*),sum(CASE WHEN status>=400 OR state IN ('failed','interrupted') THEN 1 ELSE 0 END),
 sum(coalesce(json_extract(record,'$.input'),0)),sum(coalesce(json_extract(record,'$.cache_read'),0)),
 sum(coalesce(json_extract(record,'$.cache_write'),0)),sum(coalesce(json_extract(record,'$.output'),0)),sum(coalesce(json_extract(record,'$.ms'),0))
 FROM requests WHERE kind='completion' AND state!='streaming' AND time>=? GROUP BY 1,2,3 ORDER BY 3`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var rec usage.Record
		var n, errors int
		var ms int64
		if err = rows.Scan(&rec.Model, &rec.Account, &rec.Time, &n, &errors, &rec.Input, &rec.CacheRead, &rec.CacheWrite, &rec.Output, &ms); err != nil {
			return nil, fmt.Errorf("analytics: %w", err)
		}
		out = append(out, map[string]any{"model": rec.Model, "account": rec.Account, "time": rec.Time, "requests": n, "errors": errors, "input": rec.Input, "cache_read": rec.CacheRead, "cache_write": rec.CacheWrite, "output": rec.Output, "ms": ms})
	}
	return out, rows.Err()
}
