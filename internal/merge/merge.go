package merge

import (
	"encoding/json"
	"github.com/Ltre/MusicoletWeb/internal/domain"
	"sort"
)

type Conflict struct {
	Type, Key          string
	Base, Ours, Theirs any
}

type SongDecision struct {
	Result   *domain.Song
	Conflict *Conflict
}

func MergeSong(base *domain.Song, ours *domain.Song, theirs *domain.Song) SongDecision {
	if base == nil {
		if ours == nil {
			return SongDecision{Result: theirs}
		}
		if theirs == nil {
			return SongDecision{Result: ours}
		}
		if ours.CoreKey() == theirs.CoreKey() {
			return SongDecision{Result: ours}
		}
		return SongDecision{Conflict: &Conflict{Type: "song", Key: ours.Path, Base: nil, Ours: ours, Theirs: theirs}}
	}
	bc := base.CoreKey()
	oc := "<deleted>"
	tc := "<deleted>"
	if ours != nil {
		oc = ours.CoreKey()
	}
	if theirs != nil {
		tc = theirs.CoreKey()
	}
	if tc == bc {
		return SongDecision{Result: ours}
	}
	if oc == bc {
		return SongDecision{Result: theirs}
	}
	if oc == tc {
		return SongDecision{Result: ours}
	}
	return SongDecision{Conflict: &Conflict{Type: "song", Key: base.Path, Base: base, Ours: ours, Theirs: theirs}}
}

type OrderedResult struct {
	Items     []string
	Conflicts []Conflict
}

func MergeOrdered(key string, base, ours, theirs []string) OrderedResult {
	bp, op, tp := positions(base), positions(ours), positions(theirs)
	conflicts := []Conflict{}
	removedO := setDiff(bp, op)
	removedT := setDiff(bp, tp)
	for x, bpos := range bp {
		opos, ook := op[x]
		tpos, tok := tp[x]
		if !ook && tok && tpos != bpos {
			continue
		}
		if !tok && ook && opos != bpos {
			continue
		}
		if ook && tok && opos != bpos && tpos != bpos && opos != tpos {
			conflicts = append(conflicts, Conflict{Type: "ordered_move", Key: key + ":" + x, Base: bpos, Ours: opos, Theirs: tpos})
		}
	}
	for x, opos := range op {
		if _, inBase := bp[x]; inBase {
			continue
		}
		if tpos, ok := tp[x]; ok && tpos != opos {
			conflicts = append(conflicts, Conflict{Type: "ordered_add", Key: key + ":" + x, Base: nil, Ours: opos, Theirs: tpos})
		}
	}
	// Start from ours; apply non-conflicting incoming adds/removes/moves. Server removals win over incoming moves.
	res := append([]string(nil), ours...)
	present := make(map[string]bool, len(res))
	for _, x := range res {
		present[x] = true
	}
	for x := range removedT {
		if _, serverRemoved := removedO[x]; serverRemoved {
			continue
		}
		if op[x] == bp[x] {
			res = remove(res, x)
			delete(present, x)
		}
	}
	for i, x := range theirs {
		if _, inBase := bp[x]; !inBase && !present[x] {
			res = insert(res, x, min(i, len(res)))
			present[x] = true
		}
	}
	for x, bpos := range bp {
		tpos, tok := tp[x]
		opos, ook := op[x]
		if !tok || !ook || tpos == bpos || opos != bpos {
			continue
		}
		res = move(res, x, min(tpos, len(res)-1))
	}
	return OrderedResult{Items: res, Conflicts: conflicts}
}
func MergePlayCount(previousResolve, currentServer, baseImport, newImport int64) int64 {
	return previousResolve + (currentServer - previousResolve) + (newImport - baseImport)
}
func positions(a []string) map[string]int {
	m := map[string]int{}
	for i, x := range a {
		m[x] = i
	}
	return m
}
func setDiff(a, b map[string]int) map[string]bool {
	m := map[string]bool{}
	for x := range a {
		if _, ok := b[x]; !ok {
			m[x] = true
		}
	}
	return m
}
func remove(a []string, x string) []string {
	r := a[:0]
	for _, y := range a {
		if y != x {
			r = append(r, y)
		}
	}
	return r
}
func insert(a []string, x string, i int) []string {
	a = append(a, "")
	copy(a[i+1:], a[i:])
	a[i] = x
	return a
}
func move(a []string, x string, i int) []string {
	a = remove(a, x)
	if i < 0 {
		i = 0
	}
	if i > len(a) {
		i = len(a)
	}
	return insert(a, x, i)
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func JSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func SortedKeys[M ~map[string]V, V any](m M) []string {
	r := make([]string, 0, len(m))
	for k := range m {
		r = append(r, k)
	}
	sort.Strings(r)
	return r
}
