package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
)

type ResolutionAudit struct {
	ID          int64           `json:"id"`
	ConflictID  int64           `json:"conflict_id"`
	ProcedureID int64           `json:"procedure_id"`
	TargetType  string          `json:"target_type"`
	TargetKey   string          `json:"target_key"`
	Resolution  string          `json:"resolution"`
	ServerHead  int64           `json:"server_head"`
	Base        json.RawMessage `json:"base"`
	Ours        json.RawMessage `json:"ours"`
	Theirs      json.RawMessage `json:"theirs"`
	Result      json.RawMessage `json:"result"`
	Patch       json.RawMessage `json:"patch"`
	CreatedAt   int64           `json:"created_at"`
}

func buildResolutionPatch(target string, ours, theirs, result json.RawMessage) json.RawMessage {
	out := map[string]any{"target_type": target}
	if target == "queue" || target == "playlist" || target == "queue_order" {
		var o, t, r []string
		if json.Unmarshal(ours, &o) == nil && json.Unmarshal(theirs, &t) == nil && json.Unmarshal(result, &r) == nil {
			out["from_ours"] = orderedPatch(o, r)
			out["from_theirs"] = orderedPatch(t, r)
			b, _ := json.Marshal(out)
			return b
		}
	}
	var ov, tv, rv any
	_ = json.Unmarshal(ours, &ov)
	_ = json.Unmarshal(theirs, &tv)
	_ = json.Unmarshal(result, &rv)
	out["from_ours"] = valuePatch(ov, rv)
	out["from_theirs"] = valuePatch(tv, rv)
	b, _ := json.Marshal(out)
	return b
}

func orderedPatch(from, to []string) []map[string]any {
	ops := []map[string]any{}
	toSet := map[string]bool{}
	for _, x := range to {
		toSet[x] = true
	}
	for _, x := range from {
		if !toSet[x] {
			ops = append(ops, map[string]any{"op": "remove", "item": x})
		}
	}
	work := []string{}
	for _, x := range from {
		if toSet[x] {
			work = append(work, x)
		}
	}
	for i, x := range to {
		idx := indexOf(work, x)
		if idx < 0 {
			ops = append(ops, map[string]any{"op": "add", "item": x, "position": i})
			work = append(work, "")
			copy(work[i+1:], work[i:])
			work[i] = x
		} else if idx != i {
			ops = append(ops, map[string]any{"op": "move", "item": x, "from": idx, "to": i})
			v := work[idx]
			work = append(work[:idx], work[idx+1:]...)
			work = append(work, "")
			copy(work[i+1:], work[i:])
			work[i] = v
		}
	}
	return ops
}

func indexOf(a []string, x string) int {
	for i, v := range a {
		if v == x {
			return i
		}
	}
	return -1
}

func valuePatch(from, to any) any {
	fm, fok := from.(map[string]any)
	tm, tok := to.(map[string]any)
	if fok && tok {
		keys := map[string]bool{}
		for k := range fm {
			keys[k] = true
		}
		for k := range tm {
			keys[k] = true
		}
		var names []string
		for k := range keys {
			names = append(names, k)
		}
		sort.Strings(names)
		changes := []map[string]any{}
		for _, k := range names {
			a, ao := fm[k]
			b, bo := tm[k]
			aj, _ := json.Marshal(a)
			bj, _ := json.Marshal(b)
			if ao != bo || string(aj) != string(bj) {
				changes = append(changes, map[string]any{"field": k, "before": a, "after": b})
			}
		}
		return changes
	}
	return map[string]any{"before": from, "after": to}
}

func (s *Service) ListResolutionHistory(ctx context.Context, pid int64) ([]ResolutionAudit, error) {
	rows, e := s.Store.DB.QueryContext(ctx, `SELECT r.id,r.conflict_id,r.procedure_id,r.target_type,r.target_key,r.resolution,r.server_head,COALESCE(r.base_json,'null'),COALESCE(r.ours_json,'null'),COALESCE(r.theirs_json,'null'),COALESCE(r.result_json,'null'),COALESCE(p.patch_json,'{}'),r.created_at FROM conflict_resolutions r LEFT JOIN resolution_patches p ON p.resolution_id=r.id WHERE r.procedure_id=? ORDER BY r.id`, pid)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []ResolutionAudit
	for rows.Next() {
		var x ResolutionAudit
		var base, ours, theirs, result, patch string
		if e = rows.Scan(&x.ID, &x.ConflictID, &x.ProcedureID, &x.TargetType, &x.TargetKey, &x.Resolution, &x.ServerHead, &base, &ours, &theirs, &result, &patch, &x.CreatedAt); e != nil {
			return nil, e
		}
		x.Base = json.RawMessage(base)
		x.Ours = json.RawMessage(ours)
		x.Theirs = json.RawMessage(theirs)
		x.Result = json.RawMessage(result)
		x.Patch = json.RawMessage(patch)
		out = append(out, x)
	}
	return out, rows.Err()
}

func conflictForUpdate(ctx context.Context, tx *sql.Tx, cid int64) (pid int64, typ, key string, base, ours, theirs string, err error) {
	err = tx.QueryRowContext(ctx, "SELECT procedure_id,target_type,target_key,COALESCE(base_json,'null'),COALESCE(ours_json,'null'),COALESCE(theirs_json,'null') FROM merge_conflicts WHERE id=?", cid).Scan(&pid, &typ, &key, &base, &ours, &theirs)
	return
}
