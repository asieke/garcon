package store

import (
	"database/sql"
	"encoding/json"
	"garcon/internal/usage"
	"strings"
)

func maxUsage(dst *usage.Record, src usage.Record) {
	dst.Input = max(dst.Input, src.Input)
	dst.CacheRead = max(dst.CacheRead, src.CacheRead)
	dst.CacheWrite = max(dst.CacheWrite, src.CacheWrite)
	dst.Output = max(dst.Output, src.Output)
}

// Repeated transcript blocks describe one API message. Proxy observations win
// account/status/timing attribution; provider IDs join them in either arrival
// order. Legacy proxy rows can only be joined by a unique exact usage match in
// the same session and request interval. Never infer a join from time alone.
// plausibleTime is 2001-01-01 in Unix milliseconds.
const plausibleTime = 978307200000

func reconcileClaude(tx *sql.Tx, r *usage.Record) (bool, error) {
	if r.Kind != "completion" {
		return false, nil
	}
	imported := r.Source == "claude-local" || r.Source == "muse-local" || r.Source == "agy-local"
	if imported {
		var raw string
		err := tx.QueryRow("SELECT record FROM requests WHERE request_id=?", r.RequestID).Scan(&raw)
		if err != nil && err != sql.ErrNoRows {
			return false, err
		}
		if err == nil {
			var old usage.Record
			if err = json.Unmarshal([]byte(raw), &old); err != nil {
				return false, err
			}
			maxUsage(r, old)
			// Each transcript line of one message may carry a different content
			// block; join their tool calls (in line order) and call details.
			tools := usage.MergeTools(old.Tools, r.Tools)
			usage.MergeCall(r, old)
			r.Tools = tools
			// Keep the earliest observation, unless an older reader stored an
			// implausible pre-2001 time that a corrected reader now replaces.
			if old.Time >= plausibleTime {
				r.Time = min(r.Time, old.Time)
			}
			// A later login change must not silently reattribute imported history.
			r.Account, r.AccountAttribution = old.Account, old.AccountAttribution
			r.SessionID = old.SessionID
		}
	}
	if r.Harness != "claude" {
		return false, nil
	}
	where := `json_extract(record,'$.harness')='claude' AND kind='completion' AND (request_id IS NULL OR request_id!=?) AND `
	args := []any{r.RequestID}
	if imported {
		where += `coalesce(json_extract(record,'$.source'),'')!='claude-local'`
	} else {
		where += `json_extract(record,'$.source')='claude-local'`
	}
	// Restrict the candidate set with indexed provider IDs or the session index.
	// Include only identifiers we have. Parameter guards inside each OR arm
	// prevent SQLite from using its multi-index OR plan and force a ledger scan.
	identifiers := []string{}
	for _, candidate := range []struct{ column, value string }{
		{"json_extract(record,'$.message_id')", r.MessageID},
		{"json_extract(record,'$.upstream_request_id')", r.UpstreamRequestID},
		{"session_id", r.SessionID},
	} {
		if candidate.value != "" {
			identifiers = append(identifiers, candidate.column+"=?")
			args = append(args, candidate.value)
		}
	}
	if len(identifiers) == 0 {
		return false, nil
	}
	where += " AND (" + strings.Join(identifiers, " OR ") + ")"
	rows, err := tx.Query("SELECT sequence,record FROM requests WHERE "+where, args...)
	if err != nil {
		return false, err
	}
	type match struct {
		seq int
		rec usage.Record
	}
	var exact, legacy []match
	for rows.Next() {
		var m match
		var raw string
		if err = rows.Scan(&m.seq, &raw); err != nil {
			rows.Close()
			return false, err
		}
		if err = json.Unmarshal([]byte(raw), &m.rec); err != nil {
			rows.Close()
			return false, err
		}
		other := m.rec
		if (r.MessageID != "" && r.MessageID == other.MessageID) || (r.UpstreamRequestID != "" && r.UpstreamRequestID == other.UpstreamRequestID) {
			exact = append(exact, m)
			continue
		}
		proxy, local := other, *r
		if !imported {
			proxy, local = *r, other
		}
		if proxy.MessageID != "" || proxy.UpstreamRequestID != "" || proxy.State == "streaming" {
			continue
		}
		if proxy.SessionID != "" && proxy.SessionID == local.SessionID && proxy.Model == local.Model &&
			proxy.Input == local.Input && proxy.Output == local.Output && proxy.CacheRead == local.CacheRead && proxy.CacheWrite == local.CacheWrite &&
			local.Input+local.Output+local.CacheRead+local.CacheWrite > 0 && local.Time >= proxy.Time-2000 && local.Time <= proxy.Time+max(0, proxy.Ms)+2000 {
			legacy = append(legacy, m)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	if len(exact) == 0 && len(legacy) == 1 {
		exact = legacy
	}
	if imported && len(exact) > 0 {
		// Associate provider IDs even on a legacy row, making future imports exact.
		m := exact[0]
		if r.MessageID != "" {
			m.rec.MessageID = r.MessageID
		}
		if r.UpstreamRequestID != "" {
			m.rec.UpstreamRequestID = r.UpstreamRequestID
		}
		maxUsage(&m.rec, *r)
		usage.MergeCall(&m.rec, *r)
		b, err := json.Marshal(m.rec)
		if err != nil {
			return false, err
		}
		if _, err = tx.Exec("UPDATE requests SET record=? WHERE sequence=?", string(b), m.seq); err != nil {
			return false, err
		}
		_, err = tx.Exec("DELETE FROM requests WHERE request_id=? AND json_extract(record,'$.source')='claude-local'", r.RequestID)
		return true, err
	}
	for _, m := range exact {
		maxUsage(r, m.rec)
		usage.MergeCall(r, m.rec)
		if r.MessageID == "" {
			r.MessageID = m.rec.MessageID
		}
		if r.UpstreamRequestID == "" {
			r.UpstreamRequestID = m.rec.UpstreamRequestID
		}
		if _, err = tx.Exec("DELETE FROM requests WHERE sequence=?", m.seq); err != nil {
			return false, err
		}
	}
	return false, nil
}
