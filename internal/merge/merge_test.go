package merge

import (
	"fmt"
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
	base = 116
	server = 160
	imp = 120
	r = MergePlayCount(r, server, base, imp)
	if r != 164 {
		t.Fatal(r)
	}
}

func TestMergeOrderedRealLibraryScale(t *testing.T) {
	const n = 30000
	base := make([]string, n)
	for i := range base {
		base[i] = fmt.Sprintf("S%05d", i)
	}
	ours := append([]string(nil), base...)
	// A few server moves on a very large list.
	for i := 0; i < 20; i++ {
		x := fmt.Sprintf("S%05d", 100+i)
		ours = move(ours, x, 1000+i)
	}
	theirs := append([]string(nil), base...)
	// Phone adds are distinct from the server-moved members, so this is mergeable.
	for i := 0; i < 30; i++ {
		theirs = insert(theirs, fmt.Sprintf("N%05d", i), len(theirs))
	}
	r := MergeOrdered("scale", base, ours, theirs)
	if len(r.Conflicts) != 0 {
		t.Fatalf("unexpected scale conflicts: %d", len(r.Conflicts))
	}
	if len(r.Items) != n+30 {
		t.Fatalf("merged length=%d want %d", len(r.Items), n+30)
	}
	seen := make(map[string]bool, len(r.Items))
	for _, x := range r.Items {
		if seen[x] {
			t.Fatalf("duplicate item after large merge: %s", x)
		}
		seen[x] = true
	}
}

func BenchmarkMergeOrderedRealQueueScale(b *testing.B) {
	const n = 15780
	base := make([]string, n)
	for i := range base {
		base[i] = fmt.Sprintf("S%05d", i)
	}
	ours := append([]string(nil), base...)
	for i := 0; i < 10; i++ {
		ours = move(ours, fmt.Sprintf("S%05d", 100+i), 900+i)
	}
	theirs := append([]string(nil), base...)
	for i := 0; i < 20; i++ {
		theirs = append(theirs, fmt.Sprintf("N%05d", i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := MergeOrdered("benchmark", base, ours, theirs)
		if len(r.Items) != n+20 || len(r.Conflicts) != 0 {
			b.Fatalf("unexpected merge result")
		}
	}
}
