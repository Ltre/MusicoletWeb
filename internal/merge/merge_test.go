package merge

import (
	"github.com/Ltre/MusicoletWeb/internal/domain"
	"testing"
)

func TestSongCoreConflict(t *testing.T) {
	b := domain.Song{Path: "/a", Title: "A", Artist: "X"}
	o := b
	o.Title = "B"
	th := b
	th.Artist = "Y"
	if MergeSong(&b, &o, &th).Conflict == nil {
		t.Fatal("expected song-core conflict")
	}
}
func TestOrderedMoveConflict(t *testing.T) {
	r := MergeOrdered("q", []string{"A", "B", "C", "D"}, []string{"B", "A", "C", "D"}, []string{"A", "C", "D", "B"})
	if len(r.Conflicts) == 0 {
		t.Fatal("expected move conflict")
	}
}
func TestDeleteVsMoveNoConflict(t *testing.T) {
	r := MergeOrdered("q", []string{"A", "B", "C"}, []string{"A", "C"}, []string{"B", "A", "C"})
	if len(r.Conflicts) != 0 {
		t.Fatalf("unexpected conflict %#v", r.Conflicts)
	}
	for _, x := range r.Items {
		if x == "B" {
			t.Fatal("server removal must win")
		}
	}
}
func TestPlayCounts(t *testing.T) {
	r := int64(100)
	base := int64(100)
	server := int64(102)
	imp := int64(103)
	r = MergePlayCount(r, server, base, imp)
	if r != 105 {
		t.Fatal(r)
	}
	base = 103
	server = 115
	imp = 108
	r = MergePlayCount(r, server, base, imp)
	if r != 120 {
		t.Fatal(r)
	}
	base = 108
	server = 138
	imp = 116
	r = MergePlayCount(r, server, base, imp)
	if r != 146 {
		t.Fatal(r)
	}
}
