package store

import (
	"encoding/json"
	"garcon/internal/usage"
)

// The sessions table is the Codex router's durable account assignments, not
// the inventory of all clients. Build that inventory from the request ledger
// and retain pins with no recorded requests (including migrated legacy pins).
// A session ID is scoped to its harness, never its model or upstream provider.
func (s *Store) sessions() ([]map[string]any, error) {
	rows, err := s.DB.Query(`WITH tagged AS (
 SELECT r.*,coalesce(nullif(json_extract(record,'$.harness'),''),
   CASE WHEN EXISTS (SELECT 1 FROM sessions s WHERE s.session_id=r.session_id AND s.session_id!='') THEN 'codex' ELSE '' END) AS harness
 FROM requests r WHERE session_id!=''
), observed AS (
 SELECT harness,session_id,min(time) AS created_at,
 max(time+max(0,coalesce(json_extract(record,'$.ms'),0))) AS last_seen,
 count(*) AS requests,sum(state='streaming') AS active,
 sum(state='streaming' AND harness='codex' AND model='codex-auto-review') AS reviews,
 coalesce(min(CASE WHEN state='streaming' THEN time END),0) AS active_since,
 max(CASE WHEN kind='completion' AND NOT (harness='codex' AND model='codex-auto-review') THEN sequence END) AS latest,
 max(sequence) AS last_request
 FROM tagged GROUP BY harness,session_id
), inventory AS (
 SELECT coalesce(s.key,json_array(o.harness,o.session_id)) AS key,o.harness,o.session_id,
 coalesce(nullif(s.account_id,''),r.account_id) AS account_id,
 coalesce(nullif(s.account,''),json_extract(r.record,'$.account'),'') AS account,
 coalesce(s.model,'') AS model,
 CASE WHEN s.created_at>0 THEN min(s.created_at,o.created_at) ELSE o.created_at END AS created_at,
 max(coalesce(s.last_seen,0),o.last_seen) AS last_seen,
 o.requests,o.active,o.reviews,o.active_since,coalesce(l.record,'{}') AS latest_json,
 coalesce(json_extract(r.record,'$.provider'),'') AS provider
 FROM observed o JOIN requests r ON r.sequence=o.last_request
 LEFT JOIN requests l ON l.sequence=o.latest
 LEFT JOIN sessions s ON o.harness='codex' AND s.session_id=o.session_id
 UNION ALL
 SELECT s.key,'codex',s.session_id,s.account_id,s.account,s.model,s.created_at,s.last_seen,
 0,0,0,0,'{}','chatgpt' FROM sessions s
 WHERE NOT EXISTS (SELECT 1 FROM observed o WHERE o.harness='codex' AND o.session_id=s.session_id)
)
SELECT * FROM inventory ORDER BY (active>0) DESC,last_seen DESC LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var key, harness, id, aid, account, model, latestJSON, provider string
		var created, last, activeSince int64
		var n, active, reviews int
		if err = rows.Scan(&key, &harness, &id, &aid, &account, &model, &created, &last, &n, &active, &reviews, &activeSince, &latestJSON, &provider); err != nil {
			return nil, err
		}
		var latest usage.Record
		if err = json.Unmarshal([]byte(latestJSON), &latest); err != nil {
			return nil, err
		}
		if latest.Model != "" {
			model = latest.Model
		} else if harness == "codex" && model == "codex-auto-review" {
			model = ""
		}
		if provider == "" {
			switch harness {
			case "codex":
				provider = "chatgpt"
			case "claude":
				provider = "anthropic"
			}
		}
		out = append(out, map[string]any{"key": key, "harness": harness, "provider": provider, "session_id": id, "account_id": aid, "account": account, "model": model, "created_at": created, "last_seen": last, "requests": n, "active": active, "active_reviews": reviews, "active_since": activeSince, "last_state": latest.State, "last_status": latest.Status, "last_error": latest.Error})
	}
	return out, rows.Err()
}
