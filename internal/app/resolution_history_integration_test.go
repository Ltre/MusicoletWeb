//go:build integration

package app

import (
	"context"
	"testing"
)

func TestResolutionHistoryReturnsDecisionTimeSnapshots(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	r, err := svc.Store.DB.ExecContext(ctx, `INSERT INTO conflict_resolutions(conflict_id,procedure_id,target_type,target_key,resolution,server_head,base_json,ours_json,theirs_json,result_json,created_at) VALUES(7,42,'song','/a.mp3','OURS',11,'{"title":"BASE"}','{"title":"OLD_OURS"}','{"title":"THEIRS"}','{"title":"OLD_OURS"}',123)`)
	if err != nil {
		t.Fatal(err)
	}
	rid, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Store.DB.ExecContext(ctx, `INSERT INTO resolution_patches(resolution_id,patch_json,created_at) VALUES(?, '{"from_ours":[]}', 124)`, rid); err != nil {
		t.Fatal(err)
	}
	h, err := svc.ListResolutionHistory(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 1 {
		t.Fatalf("history len=%d", len(h))
	}
	got := h[0]
	if string(got.Base) != `{"title":"BASE"}` || string(got.Ours) != `{"title":"OLD_OURS"}` || string(got.Theirs) != `{"title":"THEIRS"}` || string(got.Result) != `{"title":"OLD_OURS"}` {
		t.Fatalf("decision-time snapshots missing: base=%s ours=%s theirs=%s result=%s", got.Base, got.Ours, got.Theirs, got.Result)
	}
	if got.Resolution != "OURS" || got.ServerHead != 11 || got.ConflictID != 7 {
		t.Fatalf("resolution metadata mismatch: %#v", got)
	}
}
