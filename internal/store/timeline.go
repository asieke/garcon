package store

import (
	"encoding/json"
	"net/http"
	"slices"
)

// harnessExpr is a request's client; rows recorded before the harness field
// existed are Codex when the router assigned their session.
const harnessExpr = `coalesce(nullif(json_extract(record,'$.harness'),''),CASE WHEN EXISTS (SELECT 1 FROM sessions s WHERE s.session_id=requests.session_id AND s.session_id!='') THEN 'codex' ELSE '' END)`

// maxTimeline bounds one session's drill-down; the newest calls are kept.
const maxTimeline = 5000

// Child is one subagent seen in a session's timeline: a child thread inside the
// session, or a child conversation recorded under its own session ID.
type Child struct {
	Key             string   `json:"key"`
	SessionID       string   `json:"session_id"`
	ThreadID        string   `json:"thread_id,omitempty"`
	Subagent        string   `json:"subagent,omitempty"`
	AgentName       string   `json:"agent_name,omitempty"`
	ParentSessionID string   `json:"parent_session_id,omitempty"`
	Models          []string `json:"models"`
	Calls           int      `json:"calls"`
	ToolCalls       int      `json:"tool_calls"`
	Input           int64    `json:"input"`
	CacheRead       int64    `json:"cache_read"`
	CacheWrite      int64    `json:"cache_write"`
	Output          int64    `json:"output"`
	First           int64    `json:"first"`
	Last            int64    `json:"last"`
}

type Timeline struct {
	Harness   string `json:"harness"`
	SessionID string `json:"session_id"`
	// ParentSessionID is set when this session was started by another one.
	ParentSessionID string         `json:"parent_session_id,omitempty"`
	Requests        []RecentRecord `json:"requests"`
	Total           int            `json:"total"`
	Truncated       bool           `json:"truncated"`
	Children        []Child        `json:"children"`
}

// timeline returns a session's requests in time order with the requests of
// child sessions that name it as their parent, and a summary per subagent.
func (s *Store) timeline(r *http.Request) (Timeline, error) {
	q := r.URL.Query()
	id := q.Get("session_id")
	out := Timeline{Harness: q.Get("harness"), SessionID: id, Requests: []RecentRecord{}, Children: []Child{}}
	where := `(session_id=? OR (json_extract(record,'$.parent_session_id')=? AND session_id!=?))`
	args := []any{id, id, id}
	if q.Has("harness") {
		where += ` AND ` + harnessExpr + `=?`
		args = append(args, out.Harness)
	}
	if err := s.DB.QueryRow("SELECT count(*) FROM requests WHERE "+where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := s.DB.Query("SELECT sequence,record FROM requests WHERE "+where+" ORDER BY time DESC,sequence DESC LIMIT ?", append(args, maxTimeline)...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var rec RecentRecord
		var b string
		if err = rows.Scan(&rec.Sequence, &b); err != nil {
			return out, err
		}
		if err = json.Unmarshal([]byte(b), &rec.Record); err != nil {
			return out, err
		}
		out.Requests = append(out.Requests, rec)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	slices.Reverse(out.Requests)
	out.Truncated = out.Total > len(out.Requests)
	out.Children, out.ParentSessionID = children(id, out.Requests)
	return out, nil
}

// children summarizes each subagent thread or child session in a timeline.
func children(id string, requests []RecentRecord) ([]Child, string) {
	out := []Child{}
	index := map[string]int{}
	parent := ""
	for _, req := range requests {
		r := req.Record
		key := ""
		switch {
		case r.SessionID != id:
			key = "session:" + r.SessionID
		case r.ThreadID != "":
			key = "thread:" + r.ThreadID
		default:
			if r.ParentSessionID != "" && r.ParentSessionID != id {
				parent = r.ParentSessionID
			}
			continue
		}
		i, ok := index[key]
		if !ok {
			i = len(out)
			index[key] = i
			out = append(out, Child{Key: key, SessionID: r.SessionID, ThreadID: r.ThreadID, Models: []string{}, First: r.Time})
		}
		c := &out[i]
		fill := func(dst *string, v string) {
			if *dst == "" {
				*dst = v
			}
		}
		fill(&c.Subagent, r.Subagent)
		fill(&c.AgentName, r.AgentName)
		fill(&c.ParentSessionID, r.ParentSessionID)
		if r.Model != "" && !slices.Contains(c.Models, r.Model) {
			c.Models = append(c.Models, r.Model)
		}
		if r.Kind == "" || r.Kind == "completion" {
			c.Calls++
		}
		c.ToolCalls += len(r.Tools)
		c.Input += r.Input
		c.CacheRead += r.CacheRead
		c.CacheWrite += r.CacheWrite
		c.Output += r.Output
		c.First = min(c.First, r.Time)
		c.Last = max(c.Last, r.Time+max(0, r.Ms))
	}
	return out, parent
}
