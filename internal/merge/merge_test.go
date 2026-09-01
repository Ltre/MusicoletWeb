package merge

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Ltre/MusicoletWeb/internal/domain"
)

func TestSongCoreIsWholeConflictUnit(t *testing.T) {
	b := domain.NewSnapshot()
	o := domain.NewSnapshot()
	th := domain.NewSnapshot()
	b.Songs["/a"] = domain.Song{Path: "/a", Title: "A", Artist: "X"}
	o.Songs["/a"] = domain.Song{Path: "/a", Title: "B", Artist: "X"}
	th.Songs["/a"] = domain.Song{Path: "/a", Title: "A", Artist: "Y"}
	plan := Analyze(b, o, th)
	if len(plan.Conflicts) != 1 {
		t.Fatalf("conflicts=%d, want 1", len(plan.Conflicts))
	}
}

func TestSettingsParticipateInThreeWayMerge(t *testing.T) {
	base, ours, theirs := domain.NewSnapshot(), domain.NewSnapshot(), domain.NewSnapshot()
	base.Settings["ui.json"] = json.RawMessage(`{"theme":"dark"}`)
	ours.Settings["ui.json"] = json.RawMessage(`{"theme":"dark"}`)
	theirs.Settings["ui.json"] = json.RawMessage(`{"theme":"black"}`)
	plan := Analyze(base, ours, theirs)
	if len(plan.Conflicts) != 0 || string(plan.Merged.Settings["ui.json"]) != `{"theme":"black"}` {
		t.Fatalf("plan=%#v", plan)
	}
	if len(plan.Diffs) != 1 || plan.Diffs[0].TargetType != "setting" {
		t.Fatalf("diffs=%#v", plan.Diffs)
	}
}

func TestOrderedServerMovePhoneAdd(t *testing.T) {
	merged, conflicts := mergeOrdered([]string{"A", "B", "C", "D"}, []string{"A", "C", "B", "D"}, []string{"A", "B", "C", "D", "E"})
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %#v", conflicts)
	}
	want := []string{"A", "C", "B", "D", "E"}
	if !reflect.DeepEqual(merged, want) {
		t.Fatalf("merged=%v want=%v", merged, want)
	}
}

func TestOrderedDeleteMasksPhoneMove(t *testing.T) {
	merged, conflicts := mergeOrdered([]string{"A", "B", "C"}, []string{"A", "C"}, []string{"B", "A", "C"})
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %#v", conflicts)
	}
	if !reflect.DeepEqual(merged, []string{"A", "C"}) {
		t.Fatalf("merged=%v", merged)
	}
}

func TestPlaybackDeltaFormula(t *testing.T) {
	b := domain.NewSnapshot()
	o := domain.NewSnapshot()
	th := domain.NewSnapshot()
	b.Stats["A"] = domain.PlaybackStats{Path: "A", Total: 103}
	o.Stats["A"] = domain.PlaybackStats{Path: "A", Total: 115}
	th.Stats["A"] = domain.PlaybackStats{Path: "A", Total: 108}
	plan := Analyze(b, o, th)
	if got := plan.Merged.Stats["A"].Total; got != 120 {
		t.Fatalf("got %d want 120", got)
	}
}

func TestPlaybackDeltaSequenceFromGrillMe(t *testing.T) {
	cases := []struct{ base, server, imported, want int64 }{{100, 102, 103, 105}, {103, 115, 108, 120}, {108, 138, 116, 146}, {116, 150, 120, 154}}
	for _, tc := range cases {
		b, o, th := domain.NewSnapshot(), domain.NewSnapshot(), domain.NewSnapshot()
		b.Stats["A"] = domain.PlaybackStats{Path: "A", Total: tc.base}
		o.Stats["A"] = domain.PlaybackStats{Path: "A", Total: tc.server}
		th.Stats["A"] = domain.PlaybackStats{Path: "A", Total: tc.imported}
		if got := Analyze(b, o, th).Merged.Stats["A"].Total; got != tc.want {
			t.Fatalf("base=%d server=%d import=%d: got %d want %d", tc.base, tc.server, tc.imported, got, tc.want)
		}
	}
}

func TestBothAddAtDifferentPositionsConflicts(t *testing.T) {
	_, conflicts := mergeOrdered([]string{"A", "B"}, []string{"A", "X", "B"}, []string{"A", "B", "X"})
	if len(conflicts) != 1 {
		t.Fatalf("conflicts=%d want 1", len(conflicts))
	}
}

func TestOrderedConflictIsResolvedAsWholeList(t *testing.T) {
	base := []domain.OrderedList{{SourceKey: "q:1", Name: "Q", Paths: []string{"A", "B"}}}
	ours := []domain.OrderedList{{SourceKey: "q:1", Name: "Q", Paths: []string{"A", "X", "B"}}}
	theirs := []domain.OrderedList{{SourceKey: "q:1", Name: "Q", Paths: []string{"A", "B", "X"}}}
	_, _, conflicts := mergeLists("queue", base, ours, theirs, nil, nil)
	if len(conflicts) != 1 || conflicts[0].TargetType != "queue" || conflicts[0].TargetKey != "q:1" {
		t.Fatalf("conflicts=%#v", conflicts)
	}
	if _, ok := conflicts[0].Ours.(domain.OrderedList); !ok {
		t.Fatalf("OURS must be a complete list: %#v", conflicts[0].Ours)
	}
}

func TestNoOpImportPreservesTopLevelQueueOrder(t *testing.T) {
	base := domain.NewSnapshot()
	base.Queues = []domain.Queue{
		{OrderedList: domain.OrderedList{SourceKey: "queue:z", Name: "First", Paths: []string{"A"}}},
		{OrderedList: domain.OrderedList{SourceKey: "queue:a", Name: "Second", Paths: []string{"B"}}},
	}
	base.CurrentQueueIndex = 1
	plan := Analyze(base, base, base)
	if len(plan.Diffs) != 0 || len(plan.Conflicts) != 0 {
		t.Fatalf("no-op plan contains changes: %#v", plan)
	}
	if got := []string{plan.Merged.Queues[0].SourceKey, plan.Merged.Queues[1].SourceKey}; !reflect.DeepEqual(got, []string{"queue:z", "queue:a"}) {
		t.Fatalf("queue order=%v", got)
	}
	if plan.Merged.CurrentQueueIndex != 1 {
		t.Fatalf("current Queue index=%d", plan.Merged.CurrentQueueIndex)
	}
}
