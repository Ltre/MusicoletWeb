package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Ltre/MusicoletWeb/internal/domain"
	merger "github.com/Ltre/MusicoletWeb/internal/merge"
)

func TestSnapshotRemainsImmutableAfterServerChange(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	snap := domain.NewSnapshot()
	snap.Songs["/Music/A.mp3"] = domain.Song{Path: "/Music/A.mp3", Title: "A", Artist: "X"}
	snap.Queues = []domain.Queue{{OrderedList: domain.OrderedList{SourceKey: "q:0", Name: "Queue", Paths: []string{"/Music/A.mp3"}}}}
	snap.Settings["ui.json"] = json.RawMessage(`{"theme":"dark"}`)
	if err = st.CreateProcedure(ctx, "p1", t.TempDir(), "test", "abc", 3); err != nil {
		t.Fatal(err)
	}
	if _, err = st.PutCandidate(ctx, "p1", snap, nil); err != nil {
		t.Fatal(err)
	}
	if err = st.CommitProcedure(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	if err = st.UpdateSong(ctx, "/Music/A.mp3", map[string]any{"title": "Server A"}); err != nil {
		t.Fatal(err)
	}
	working, err := st.LoadWorking(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if working.Songs["/Music/A.mp3"].Title != "Server A" {
		t.Fatalf("working title=%q", working.Songs["/Music/A.mp3"].Title)
	}
	if string(working.Settings["ui.json"]) != `{"theme":"dark"}` {
		t.Fatalf("working settings were not materialized: %s", working.Settings["ui.json"])
	}
	version, err := st.LoadVersion(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if version.Songs["/Music/A.mp3"].Title != "A" {
		t.Fatalf("snapshot was mutated: %q", version.Songs["/Music/A.mp3"].Title)
	}
}

func TestResolutionBecomesStaleWhenOursChanges(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	base := domain.NewSnapshot()
	base.Songs["A"] = domain.Song{Path: "A", Title: "Base"}
	if err = st.CreateProcedure(ctx, "p1", t.TempDir(), "test", "one", 1); err != nil {
		t.Fatal(err)
	}
	if _, err = st.PutCandidate(ctx, "p1", base, nil); err != nil {
		t.Fatal(err)
	}
	if err = st.CommitProcedure(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	if err = st.UpdateSong(ctx, "A", map[string]any{"title": "Server 1"}); err != nil {
		t.Fatal(err)
	}
	theirs := domain.NewSnapshot()
	theirs.Songs["A"] = domain.Song{Path: "A", Title: "Phone"}
	if err = st.CreateProcedure(ctx, "p2", t.TempDir(), "test", "two", 1); err != nil {
		t.Fatal(err)
	}
	if _, err = st.PutCandidate(ctx, "p2", theirs, nil); err != nil {
		t.Fatal(err)
	}
	working, _ := st.LoadWorking(ctx)
	revision, _ := st.ServerRevision(ctx)
	plan := merger.Analyze(base, working, theirs)
	if len(plan.Conflicts) != 1 {
		t.Fatalf("conflicts=%d", len(plan.Conflicts))
	}
	if err = st.SaveAnalysis(ctx, "p2", plan, revision); err != nil {
		t.Fatal(err)
	}
	if err = st.ResolveConflict(ctx, "p2", plan.Conflicts[0].ID, "OURS", json.RawMessage("null"), nil); err != nil {
		t.Fatal(err)
	}
	if err = st.UpdateSong(ctx, "A", map[string]any{"title": "Server 2"}); err != nil {
		t.Fatal(err)
	}
	working, _ = st.LoadWorking(ctx)
	revision, _ = st.ServerRevision(ctx)
	if err = st.SaveAnalysis(ctx, "p2", merger.Analyze(base, working, theirs), revision); err != nil {
		t.Fatal(err)
	}
	rows, err := st.Conflicts(ctx, "p2")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].Stale {
		t.Fatalf("resolution stale state: %#v", rows)
	}
}

func TestQueueTailMovesExistingWithoutDuplicate(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	snap := domain.NewSnapshot()
	for _, p := range []string{"A", "B", "C"} {
		snap.Songs[p] = domain.Song{Path: p, Title: p}
	}
	if err = st.CreateProcedure(ctx, "p1", t.TempDir(), "test", "abc", 3); err != nil {
		t.Fatal(err)
	}
	if _, err = st.PutCandidate(ctx, "p1", snap, nil); err != nil {
		t.Fatal(err)
	}
	if err = st.CommitProcedure(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	id, err := st.CreateQueue(ctx, "Q", "", "", []string{"A", "B", "C"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.SetQueueItems(ctx, id, []string{"B"}, "tail"); err != nil {
		t.Fatal(err)
	}
	got, err := listItems(ctx, st.DB, "queue_items", "queue_id", id, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"A", "C", "B"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestQueueImportResolutionPreservesPlaybackState(t *testing.T) {
	working := domain.NewSnapshot()
	working.Queues = []domain.Queue{{
		OrderedList: domain.OrderedList{SourceKey: "q:1", Name: "Server Q", Paths: []string{"A", "B"}},
		Current:     1,
		ProgressMS:  1234,
		StopPath:    "B",
	}}
	theirs, _ := json.Marshal(domain.OrderedList{SourceKey: "q:1", Name: "Phone Q", Paths: []string{"B", "A"}})
	applyResolution(&working, ConflictRow{TargetType: "queue", TargetKey: "q:1", Choice: "THEIRS", Theirs: theirs})
	queue := working.Queues[0]
	if queue.Name != "Phone Q" || !reflect.DeepEqual(queue.Paths, []string{"B", "A"}) {
		t.Fatalf("list resolution failed: %#v", queue)
	}
	if queue.Current != 1 || queue.ProgressMS != 1234 || queue.StopPath != "B" {
		t.Fatalf("playback state was overwritten: %#v", queue)
	}
}

func TestQueueOrderAndSourceAssociationReuse(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	q1, err := st.CreateQueue(ctx, "One", "", "", []string{"A"}, false)
	if err != nil {
		t.Fatal(err)
	}
	q2, err := st.CreateQueue(ctx, "Two", "", "", []string{"B"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.ReorderQueues(ctx, []int64{q2, q1}); err != nil {
		t.Fatal(err)
	}
	if err = st.ReorderQueues(ctx, []int64{q2, q2}); err == nil {
		t.Fatal("duplicate Queue order was accepted")
	}
	working, err := st.LoadWorking(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(working.Queues) != 2 || working.Queues[0].ID != q2 || working.Queues[1].ID != q1 {
		t.Fatalf("queue order=%#v", working.Queues)
	}

	linked, err := st.CreateQueue(ctx, "Album", "album", "album-key", []string{"A", "B", "C"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.SetQueueItems(ctx, linked, []string{"C", "A"}, "replace"); err != nil {
		t.Fatal(err)
	}
	again, err := st.CreateQueue(ctx, "Album", "album", "album-key", []string{"B", "A", "C"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if again != linked {
		t.Fatalf("associated Queue was not reused: %d != %d", again, linked)
	}
	got, err := listItems(ctx, st.DB, "queue_items", "queue_id", linked, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"C", "A"}) {
		t.Fatalf("reused Queue was reconstructed: %v", got)
	}
}

func TestSourceQueueAssociationSurvivesImportCommit(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	base := domain.NewSnapshot()
	base.Songs["A"] = domain.Song{Path: "A", Title: "A"}
	if err = st.CreateProcedure(ctx, "p1", t.TempDir(), "test", "one", 1); err != nil {
		t.Fatal(err)
	}
	if _, err = st.PutCandidate(ctx, "p1", base, nil); err != nil {
		t.Fatal(err)
	}
	if err = st.CommitProcedure(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	if _, err = st.CreateQueue(ctx, "Album", "album", "album:a", []string{"A"}, false); err != nil {
		t.Fatal(err)
	}
	if err = st.CreateProcedure(ctx, "p2", t.TempDir(), "test", "two", 1); err != nil {
		t.Fatal(err)
	}
	if _, err = st.PutCandidate(ctx, "p2", base, nil); err != nil {
		t.Fatal(err)
	}
	ours, _ := st.LoadWorking(ctx)
	revision, _ := st.ServerRevision(ctx)
	if err = st.SaveAnalysis(ctx, "p2", merger.Analyze(base, ours, base), revision); err != nil {
		t.Fatal(err)
	}
	if err = st.CommitProcedure(ctx, "p2"); err != nil {
		t.Fatal(err)
	}
	before, _ := st.LoadWorking(ctx)
	id, err := st.CreateQueue(ctx, "Should not be created", "album", "album:a", []string{"A"}, true)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := st.LoadWorking(ctx)
	if len(after.Queues) != len(before.Queues) || len(after.Queues) != 1 || after.Queues[0].ID != id {
		t.Fatalf("association was lost: before=%#v after=%#v id=%d", before.Queues, after.Queues, id)
	}
}

func TestQueuePlaybackStateInitializesAndSurvivesImport(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	base := domain.NewSnapshot()
	for _, path := range []string{"A", "B", "C"} {
		base.Songs[path] = domain.Song{Path: path, Title: path}
	}
	base.Queues = []domain.Queue{
		{OrderedList: domain.OrderedList{SourceKey: "q:0", Name: "Q0", Paths: []string{"A"}}},
		{OrderedList: domain.OrderedList{SourceKey: "q:1", Name: "Q1", Paths: []string{"B", "C"}}, Current: 1, ProgressMS: 456},
	}
	base.CurrentQueueIndex = 1
	if err = st.CreateProcedure(ctx, "p1", t.TempDir(), "test", "one", 1); err != nil {
		t.Fatal(err)
	}
	if _, err = st.PutCandidate(ctx, "p1", base, nil); err != nil {
		t.Fatal(err)
	}
	if err = st.CommitProcedure(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	working, _ := st.LoadWorking(ctx)
	playback, _ := st.Playback(ctx)
	if working.CurrentQueueIndex != 1 || playback.QueueID != working.Queues[1].ID || playback.SongPath != "C" || playback.ProgressMS != 456 {
		t.Fatalf("initial playback: working=%#v playback=%#v", working.Queues, playback)
	}
	playback.Playing = true
	playback.ProgressMS = 777
	if err = st.UpdatePlayback(ctx, playback); err != nil {
		t.Fatal(err)
	}
	if err = st.CreateProcedure(ctx, "p2", t.TempDir(), "test", "two", 1); err != nil {
		t.Fatal(err)
	}
	if _, err = st.PutCandidate(ctx, "p2", base, nil); err != nil {
		t.Fatal(err)
	}
	working, _ = st.LoadWorking(ctx)
	revision, _ := st.ServerRevision(ctx)
	if err = st.SaveAnalysis(ctx, "p2", merger.Analyze(base, working, base), revision); err != nil {
		t.Fatal(err)
	}
	if err = st.CommitProcedure(ctx, "p2"); err != nil {
		t.Fatal(err)
	}
	working, _ = st.LoadWorking(ctx)
	playback, _ = st.Playback(ctx)
	if working.CurrentQueueIndex != 1 || playback.QueueID != working.Queues[1].ID || !playback.Playing || playback.ProgressMS != 777 {
		t.Fatalf("preserved playback: working=%#v playback=%#v", working.Queues, playback)
	}
	version, err := st.LoadVersion(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if version.CurrentQueueIndex != 1 {
		t.Fatalf("historical source current Queue=%d", version.CurrentQueueIndex)
	}
}

func TestCancelledProcedureCannotBeResurrectedByBackgroundWork(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err = st.CreateProcedure(ctx, "cancelled", t.TempDir(), "test", "archive", 1); err != nil {
		t.Fatal(err)
	}
	if err = st.CancelProcedure(ctx, "cancelled"); err != nil {
		t.Fatal(err)
	}
	snap := domain.NewSnapshot()
	snap.Songs["A"] = domain.Song{Path: "A", Title: "A"}
	if _, err = st.PutCandidate(ctx, "cancelled", snap, nil); err == nil {
		t.Fatal("cancelled procedure accepted a late candidate")
	}
	if err = st.FailProcedure(ctx, "cancelled", context.Canceled); err != nil {
		t.Fatal(err)
	}
	if err = st.SaveAnalysis(ctx, "cancelled", merger.Analyze(domain.NewSnapshot(), domain.NewSnapshot(), snap), 0); err == nil {
		t.Fatal("cancelled procedure accepted late analysis")
	}
	var status string
	if err = st.DB.QueryRowContext(ctx, "SELECT status FROM import_procedures WHERE id='cancelled'").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "CANCELLED" {
		t.Fatalf("status=%q, want CANCELLED", status)
	}
}
