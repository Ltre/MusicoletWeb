package merge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/Ltre/MusicoletWeb/internal/domain"
)

type Diff struct {
	TargetType string `json:"targetType"`
	TargetKey  string `json:"targetKey"`
	ChangeKind string `json:"changeKind"`
	Detail     any    `json:"detail"`
}

type Conflict struct {
	ID         string `json:"id"`
	TargetType string `json:"targetType"`
	TargetKey  string `json:"targetKey"`
	Reason     string `json:"reason"`
	Base       any    `json:"base"`
	Ours       any    `json:"ours"`
	Theirs     any    `json:"theirs"`
}

type Plan struct {
	Merged    domain.Snapshot `json:"merged"`
	Diffs     []Diff          `json:"diffs"`
	Conflicts []Conflict      `json:"conflicts"`
}

// Analyze applies the product's BASE/OURS/THEIRS rules. Song Core is an
// indivisible conflict unit; ordered lists use member semantics; playback
// state is not part of Snapshot and therefore always remains server-owned.
func Analyze(base, ours, theirs domain.Snapshot) Plan {
	plan := Plan{Merged: clone(ours)}
	if plan.Merged.Songs == nil {
		plan.Merged = domain.NewSnapshot()
	}
	allPaths := unionKeys(base.Songs, ours.Songs, theirs.Songs)
	for _, path := range allPaths {
		b, bOK := base.Songs[path]
		o, oOK := ours.Songs[path]
		t, tOK := theirs.Songs[path]
		if oOK && o.Deleted {
			oOK = false
		}
		if tOK && t.Deleted {
			tOK = false
		}
		switch {
		case !bOK && tOK && !oOK:
			plan.Merged.Songs[path] = t
			plan.Diffs = append(plan.Diffs, Diff{"song", path, "ADD", t})
		case !bOK && tOK && oOK:
			if !sameCore(o, t) {
				plan.Conflicts = append(plan.Conflicts, conflict("song", path, "both sides added the path with different Song Core", nil, o, t))
			} else {
				plan.Merged.Songs[path] = o
			}
		case bOK && !tOK && oOK:
			if sameCore(o, b) {
				song := o
				song.Deleted = true
				song.HasServerChanges = false
				plan.Merged.Songs[path] = song
				plan.Diffs = append(plan.Diffs, Diff{"song", path, "DELETE", b})
			} else {
				plan.Conflicts = append(plan.Conflicts, conflict("song", path, "Musicolet deleted a Song Core modified on the server", b, o, nil))
			}
		case bOK && tOK && !oOK:
			// A server delete masks an unchanged/metadata-only source row only when
			// source Song Core stayed unchanged. Playback count is handled below.
			if sameCore(t, b) {
				if existing, ok := ours.Songs[path]; ok {
					plan.Merged.Songs[path] = existing
				}
			}
			if !sameCore(t, b) {
				plan.Conflicts = append(plan.Conflicts, conflict("song", path, "server delete and Musicolet Song Core modification", b, nil, t))
			}
		case bOK && tOK && oOK:
			oursChanged := !sameCore(o, b)
			theirsChanged := !sameCore(t, b)
			switch {
			case !theirsChanged:
				plan.Merged.Songs[path] = o
			case !oursChanged:
				preserve := t
				// The source Song Core may advance while an independent Favorite,
				// playback-stat or relation M remains active on this song.
				preserve.HasServerChanges = o.HasServerChanges
				plan.Merged.Songs[path] = preserve
				plan.Diffs = append(plan.Diffs, Diff{"song", path, "MODIFY", map[string]any{"before": b, "after": t}})
			case sameCore(o, t):
				plan.Merged.Songs[path] = o
			default:
				plan.Conflicts = append(plan.Conflicts, conflict("song", path, "both sides modified Song Core", b, o, t))
			}
		}
		// Favorite is deliberately outside Song Core.
		if bOK && tOK && oOK {
			bf, of, tf := b.Favorite, o.Favorite, t.Favorite
			song := plan.Merged.Songs[path]
			switch {
			case tf == bf:
				song.Favorite = of
			case of == bf:
				song.Favorite = tf
			case of == tf:
				song.Favorite = of
			default:
				plan.Conflicts = append(plan.Conflicts, conflict("favorite", path, "both sides changed favorite differently", bf, of, tf))
			}
			if tf != bf {
				plan.Diffs = append(plan.Diffs, Diff{"favorite", path, "MODIFY", map[string]any{"base": bf, "import": tf, "resolved": song.Favorite}})
			}
			plan.Merged.Songs[path] = song
		}
	}

	plan.Merged.Playlists, plan.Diffs, plan.Conflicts = mergeLists("playlist", base.Playlists, ours.Playlists, theirs.Playlists, plan.Diffs, plan.Conflicts)
	baseQueues := queueLists(base.Queues)
	oursQueues := queueLists(ours.Queues)
	theirsQueues := queueLists(theirs.Queues)
	queueMerged, diffs, conflicts := mergeLists("queue", baseQueues, oursQueues, theirsQueues, plan.Diffs, plan.Conflicts)
	plan.Diffs, plan.Conflicts = diffs, conflicts
	plan.Merged.Queues = restoreQueues(queueMerged, ours.Queues, theirs.Queues)

	for _, path := range unionStats(base.Stats, ours.Stats, theirs.Stats) {
		b := base.Stats[path]
		o := ours.Stats[path]
		t := theirs.Stats[path]
		if o.Path == "" {
			o.Path = path
		}
		if t.Path == "" {
			t.Path = path
		}
		merged := o
		merged.Total = o.Total + (t.Total - b.Total)
		if merged.Total < 0 {
			merged.Total = 0
		}
		merged.LastPlayed = max(o.LastPlayed, t.LastPlayed)
		merged.Weekly = mergeCounterMap(b.Weekly, o.Weekly, t.Weekly)
		merged.Monthly = mergeCounterMap(b.Monthly, o.Monthly, t.Monthly)
		merged.Yearly = mergeCounterMap(b.Yearly, o.Yearly, t.Yearly)
		plan.Merged.Stats[path] = merged
		if !reflect.DeepEqual(b, t) {
			plan.Diffs = append(plan.Diffs, Diff{"playback_stats", path, "DELTA", map[string]any{"base": b, "import": t, "resolved": merged}})
		}
	}

	for _, key := range unionSettings(base.Settings, ours.Settings, theirs.Settings) {
		b, bOK := base.Settings[key]
		o, oOK := ours.Settings[key]
		t, tOK := theirs.Settings[key]
		switch {
		case rawEqual(o, oOK, t, tOK):
			if oOK {
				plan.Merged.Settings[key] = append(json.RawMessage(nil), o...)
			} else {
				delete(plan.Merged.Settings, key)
			}
		case rawEqual(t, tOK, b, bOK):
			// Source did not change: keep the server state.
		case rawEqual(o, oOK, b, bOK):
			if tOK {
				plan.Merged.Settings[key] = append(json.RawMessage(nil), t...)
			} else {
				delete(plan.Merged.Settings, key)
			}
		default:
			plan.Conflicts = append(plan.Conflicts, conflict("setting", key, "both sides changed setting differently", rawOrNil(b, bOK), rawOrNil(o, oOK), rawOrNil(t, tOK)))
		}
		if !rawEqual(t, tOK, b, bOK) {
			plan.Diffs = append(plan.Diffs, Diff{"setting", key, settingChange(bOK, tOK), map[string]any{"base": rawOrNil(b, bOK), "import": rawOrNil(t, tOK)}})
		}
	}
	plan.Merged.Normalize()
	return plan
}

func unionSettings(maps ...map[string]json.RawMessage) []string {
	seen := map[string]struct{}{}
	for _, values := range maps {
		for key := range values {
			seen[key] = struct{}{}
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func rawEqual(a json.RawMessage, aOK bool, b json.RawMessage, bOK bool) bool {
	return aOK == bOK && (!aOK || bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b)))
}

func rawOrNil(value json.RawMessage, ok bool) any {
	if !ok {
		return nil
	}
	return value
}

func settingChange(baseOK, theirsOK bool) string {
	if !baseOK {
		return "ADD"
	}
	if !theirsOK {
		return "DELETE"
	}
	return "MODIFY"
}

func sameCore(a, b domain.Song) bool { return reflect.DeepEqual(a.Core(), b.Core()) }

func conflict(targetType, targetKey, reason string, base, ours, theirs any) Conflict {
	// The identity is stable across re-analysis so a previous resolution can be
	// retained and explicitly marked stale when OURS changes.
	raw, _ := json.Marshal([]any{targetType, targetKey, reason})
	sum := sha256.Sum256(raw)
	return Conflict{ID: hex.EncodeToString(sum[:12]), TargetType: targetType, TargetKey: targetKey, Reason: reason, Base: base, Ours: ours, Theirs: theirs}
}

func clone(s domain.Snapshot) domain.Snapshot {
	raw, _ := json.Marshal(s)
	var out domain.Snapshot
	_ = json.Unmarshal(raw, &out)
	if out.Songs == nil {
		out.Songs = map[string]domain.Song{}
	}
	if out.Stats == nil {
		out.Stats = map[string]domain.PlaybackStats{}
	}
	if out.Settings == nil {
		out.Settings = map[string]json.RawMessage{}
	}
	return out
}

func unionKeys(maps ...map[string]domain.Song) []string {
	seen := map[string]struct{}{}
	for _, m := range maps {
		for k := range m {
			seen[k] = struct{}{}
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func unionStats(maps ...map[string]domain.PlaybackStats) []string {
	seen := map[string]struct{}{}
	for _, m := range maps {
		for k := range m {
			seen[k] = struct{}{}
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func mergeCounterMap(base, ours, theirs map[string]int64) map[string]int64 {
	out := map[string]int64{}
	keys := map[string]struct{}{}
	for k := range base {
		keys[k] = struct{}{}
	}
	for k := range ours {
		keys[k] = struct{}{}
	}
	for k := range theirs {
		keys[k] = struct{}{}
	}
	for k := range keys {
		out[k] = ours[k] + (theirs[k] - base[k])
		if out[k] < 0 {
			out[k] = 0
		}
	}
	return out
}

func indexMap(paths []string) map[string]int {
	out := make(map[string]int, len(paths))
	for i, path := range paths {
		out[path] = i
	}
	return out
}

func mergeOrdered(base, ours, theirs []string) ([]string, []Conflict) {
	bp, op, tp := indexMap(base), indexMap(ours), indexMap(theirs)
	om, tm := movedMembers(base, ours), movedMembers(base, theirs)
	result := append([]string(nil), ours...)
	var conflicts []Conflict
	// Source deletions apply when server did not move the member. A server
	// deletion wins over a source move, per the confirmed product rule.
	for _, path := range base {
		bIndex := bp[path]
		oi, oOK := op[path]
		ti, tOK := tp[path]
		if !oOK {
			remove(&result, path)
			continue
		}
		if !tOK {
			if oi == bIndex {
				remove(&result, path)
			} else {
				conflicts = append(conflicts, conflict("ordered_member", path, "server moved while Musicolet removed", bIndex, oi, nil))
			}
			continue
		}
		oMoved, tMoved := om[path], tm[path]
		if oMoved && tMoved && oi != ti {
			conflicts = append(conflicts, conflict("ordered_member", path, "both sides moved member to different positions", bIndex, oi, ti))
		} else if !oMoved && tMoved {
			move(&result, path, ti)
		}
	}
	// Musicolet additions are inserted; simultaneous different-position adds conflict.
	for _, path := range theirs {
		ti := tp[path]
		if _, inBase := bp[path]; inBase {
			continue
		}
		oi, inOurs := op[path]
		if inOurs && oi != ti {
			conflicts = append(conflicts, conflict("ordered_member", path, "both sides added member at different positions", nil, oi, ti))
			continue
		}
		if !inOurs {
			insert(&result, path, ti)
		}
	}
	return unique(result), conflicts
}

// movedMembers uses an LCS after excluding additions/deletions. This avoids
// treating every shifted index as a MOVE when another member was inserted,
// removed or moved around it.
func movedMembers(base, side []string) map[string]bool {
	bm, sm := indexMap(base), indexMap(side)
	var a, b []string
	for _, value := range base {
		if _, ok := sm[value]; ok {
			a = append(a, value)
		}
	}
	for _, value := range side {
		if _, ok := bm[value]; ok {
			b = append(b, value)
		}
	}
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] > dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	stable := map[string]bool{}
	for i, j := 0, 0; i < len(a) && j < len(b); {
		if a[i] == b[j] {
			stable[a[i]] = true
			i++
			j++
		} else if dp[i+1][j] > dp[i][j+1] {
			i++
		} else {
			j++
		}
	}
	moved := map[string]bool{}
	for _, value := range a {
		if !stable[value] {
			moved[value] = true
		}
	}
	return moved
}

func mergeLists(kind string, base, ours, theirs []domain.OrderedList, diffs []Diff, conflicts []Conflict) ([]domain.OrderedList, []Diff, []Conflict) {
	bm, om, tm := listMap(base), listMap(ours), listMap(theirs)
	// Top-level list order is user-visible. Preserve the server order, then
	// append source-only and finally base-only keys in their original order.
	// Sorting by source key would silently move the current Queue even when an
	// import has no semantic changes.
	ordered := make([]string, 0, len(bm)+len(om)+len(tm))
	seen := map[string]bool{}
	appendKeys := func(lists []domain.OrderedList) {
		for _, list := range lists {
			if !seen[list.SourceKey] {
				seen[list.SourceKey] = true
				ordered = append(ordered, list.SourceKey)
			}
		}
	}
	appendKeys(ours)
	appendKeys(theirs)
	appendKeys(base)
	out := make([]domain.OrderedList, 0, len(ordered))
	for _, key := range ordered {
		b, bOK := bm[key]
		o, oOK := om[key]
		t, tOK := tm[key]
		switch {
		case !bOK && tOK && !oOK:
			out = append(out, t)
			diffs = append(diffs, Diff{kind, key, "ADD", t})
		case bOK && !tOK && oOK:
			if reflect.DeepEqual(b.Paths, o.Paths) {
				diffs = append(diffs, Diff{kind, key, "DELETE", b})
			} else {
				conflicts = append(conflicts, conflict(kind, key, "Musicolet removed list modified on server", b, o, nil))
				out = append(out, o)
			}
		case bOK && tOK && !oOK:
			if reflect.DeepEqual(b.Paths, t.Paths) {
			} else {
				conflicts = append(conflicts, conflict(kind, key, "server removed list modified by Musicolet", b, nil, t))
			}
		case !bOK && oOK && !tOK:
			out = append(out, o)
		case !bOK && oOK && tOK:
			merged, cs := mergeOrdered(nil, o.Paths, t.Paths)
			o.Paths = merged
			var reasons []string
			for _, c := range cs {
				reasons = append(reasons, c.Reason)
			}
			if o.Name != t.Name {
				reasons = append(reasons, "both sides added the list with different names")
			}
			if len(reasons) > 0 {
				// A list is the resolution unit. Member-level conflicts are useful
				// diagnostics, but storing them as separately resolvable rows would
				// make OURS/THEIRS unable to restore a coherent ordering.
				conflicts = append(conflicts, conflict(kind, key, strings.Join(reasons, "; "), nil, om[key], tm[key]))
			}
			out = append(out, o)
		case bOK && oOK && tOK:
			merged, cs := mergeOrdered(b.Paths, o.Paths, t.Paths)
			o.Paths = merged
			var reasons []string
			if o.Name == b.Name {
				o.Name = t.Name
			} else if t.Name != b.Name && o.Name != t.Name {
				reasons = append(reasons, "both sides renamed the list differently")
			}
			for _, c := range cs {
				reasons = append(reasons, c.Reason)
			}
			if len(reasons) > 0 {
				conflicts = append(conflicts, conflict(kind, key, strings.Join(reasons, "; "), b, om[key], tm[key]))
			}
			if !reflect.DeepEqual(b.Paths, t.Paths) {
				diffs = append(diffs, Diff{kind, key, "MEMBERS", map[string]any{"base": b.Paths, "theirs": t.Paths, "merged": merged}})
			}
			out = append(out, o)
		}
	}
	return out, diffs, conflicts
}

func listMap(lists []domain.OrderedList) map[string]domain.OrderedList {
	out := make(map[string]domain.OrderedList, len(lists))
	for _, list := range lists {
		out[list.SourceKey] = list
	}
	return out
}
func queueLists(queues []domain.Queue) []domain.OrderedList {
	out := make([]domain.OrderedList, 0, len(queues))
	for _, q := range queues {
		out = append(out, q.OrderedList)
	}
	return out
}
func restoreQueues(lists []domain.OrderedList, ours, theirs []domain.Queue) []domain.Queue {
	om, tm := map[string]domain.Queue{}, map[string]domain.Queue{}
	for _, q := range ours {
		om[q.SourceKey] = q
	}
	for _, q := range theirs {
		tm[q.SourceKey] = q
	}
	out := make([]domain.Queue, 0, len(lists))
	for i, list := range lists {
		q, ok := om[list.SourceKey]
		if !ok {
			q = tm[list.SourceKey]
		}
		q.OrderedList = list
		q.Position = i
		out = append(out, q)
	}
	return out
}

func remove(values *[]string, target string) {
	out := (*values)[:0]
	for _, v := range *values {
		if v != target {
			out = append(out, v)
		}
	}
	*values = out
}
func insert(values *[]string, value string, index int) {
	remove(values, value)
	if index < 0 {
		index = 0
	}
	if index > len(*values) {
		index = len(*values)
	}
	*values = append(*values, "")
	copy((*values)[index+1:], (*values)[index:])
	(*values)[index] = value
}
func move(values *[]string, value string, index int) {
	remove(values, value)
	insert(values, value, index)
}
func unique(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}
