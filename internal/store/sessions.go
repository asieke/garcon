package store

import (
	"context"
	"encoding/json"
	"garcon/internal/usage"
)

// The sessions table is the Codex router's durable account assignments, not
// the inventory of all clients. Build that inventory from the request ledger
// and retain pins with no recorded requests (including migrated legacy pins).
// A session ID is scoped to its harness, never its model or upstream provider.
//
// Background reviews (the codex-auto-review model, or a review/guardian
// subagent) never replace the conversation's model. Child agent threads in the
// same session are counted as subagents and are only a fallback for the model.
func (s *Store) sessions() ([]map[string]any, error) {
	return s.sessionsContext(context.Background())
}

func (s *Store) sessionsContext(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx, `WITH tagged AS (
 SELECT r.*,coalesce(nullif(json_extract(record,'$.harness'),''),
   CASE WHEN EXISTS (SELECT 1 FROM sessions s WHERE s.session_id=r.session_id AND s.session_id!='') THEN 'codex' ELSE '' END) AS harness,
 coalesce(json_extract(record,'$.subagent'),'') AS subagent,coalesce(json_extract(record,'$.thread_id'),'') AS thread
 FROM requests r WHERE session_id!=''
), flagged AS (
 SELECT *,(harness='codex' AND (model='codex-auto-review' OR subagent IN ('review','guardian'))) AS review FROM tagged
), ordered AS (
 SELECT *,row_number() OVER (PARTITION BY harness,session_id ORDER BY time DESC,sequence DESC) AS newest,
 row_number() OVER (PARTITION BY harness,session_id ORDER BY CASE WHEN kind='completion' AND NOT review AND thread='' AND subagent='' THEN 0 WHEN kind='completion' AND NOT review THEN 1 ELSE 2 END,time DESC,sequence DESC) AS completion
 FROM flagged
), observed AS (
 SELECT harness,session_id,min(time) AS created_at,
 max(time+max(0,coalesce(json_extract(record,'$.ms'),0))) AS last_seen,
 count(*) AS requests,sum(state='streaming') AS active,
 sum(state='streaming' AND review) AS reviews,
 coalesce(min(CASE WHEN state='streaming' THEN time END),0) AS active_since,
 max(CASE WHEN completion=1 AND kind='completion' AND NOT review THEN sequence END) AS latest,
 max(CASE WHEN newest=1 THEN sequence END) AS last_request,
 sum(coalesce(json_array_length(record,'$.tools'),0)) AS tool_calls,
 count(DISTINCT nullif(thread,'')) AS threads,
 coalesce(max(CASE WHEN thread='' AND json_extract(record,'$.parent_session_id')!=session_id THEN json_extract(record,'$.parent_session_id') END),'') AS parent
 FROM ordered GROUP BY harness,session_id
), inventory AS (
 SELECT coalesce(s.key,json_array(o.harness,o.session_id)) AS key,o.harness,o.session_id,
 coalesce(nullif(s.account_id,''),r.account_id) AS account_id,
 coalesce(nullif(s.account,''),json_extract(r.record,'$.account'),'') AS account,
 coalesce(s.model,'') AS model,
 CASE WHEN s.created_at>0 THEN min(s.created_at,o.created_at) ELSE o.created_at END AS created_at,
 max(coalesce(s.last_seen,0),o.last_seen) AS last_seen,
 o.requests,o.active,o.reviews,o.active_since,coalesce(l.record,'{}') AS latest_json,
 coalesce(json_extract(r.record,'$.provider'),'') AS provider,o.tool_calls,o.threads,o.parent
 FROM observed o JOIN requests r ON r.sequence=o.last_request
 LEFT JOIN requests l ON l.sequence=o.latest
 LEFT JOIN sessions s ON o.harness='codex' AND s.session_id=o.session_id
 UNION ALL
 SELECT s.key,'codex',s.session_id,s.account_id,s.account,s.model,s.created_at,s.last_seen,
 0,0,0,0,'{}','chatgpt',0,0,'' FROM sessions s
 WHERE NOT EXISTS (SELECT 1 FROM observed o WHERE o.harness='codex' AND o.session_id=s.session_id)
)
SELECT * FROM inventory ORDER BY (active>0) DESC,last_seen DESC LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var key, harness, id, aid, account, model, latestJSON, provider, parent string
		var created, last, activeSince int64
		var n, active, reviews, toolCalls, threads int
		if err = rows.Scan(&key, &harness, &id, &aid, &account, &model, &created, &last, &n, &active, &reviews, &activeSince, &latestJSON, &provider, &toolCalls, &threads, &parent); err != nil {
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
			case "agy":
				provider = "google"
			case "muse":
				provider = "meta"
			}
		}
		out = append(out, map[string]any{"key": key, "harness": harness, "provider": provider, "session_id": id, "account_id": aid, "account": account, "model": model, "created_at": created, "last_seen": last, "requests": n, "active": active, "active_reviews": reviews, "active_since": activeSince, "last_state": latest.State, "last_status": latest.Status, "last_error": latest.Error, "source": latest.Source, "account_attribution": latest.AccountAttribution,
			"tool_calls": toolCalls, "subagents": threads, "parent_session_id": parent})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// Child agents recorded as their own sessions also count toward their parent.
	index := map[[2]string]map[string]any{}
	for _, row := range out {
		index[[2]string{row["harness"].(string), row["session_id"].(string)}] = row
	}
	for _, row := range out {
		if p, _ := row["parent_session_id"].(string); p != "" {
			if parent := index[[2]string{row["harness"].(string), p}]; parent != nil {
				parent["subagents"] = parent["subagents"].(int) + 1
			}
		}
	}
	return out, nil
}
