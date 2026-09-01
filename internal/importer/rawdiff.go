package importer

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type RawDiff struct {
	File, Text string
	Truncated  bool
}

// CompareCanonicalFiles provides the requested character/line view over
// decrypted structures. SQLite and binary formats have already been converted
// to deterministic text by ParseArchive.
func CompareCanonicalFiles(baseDir, nextDir string) ([]RawDiff, error) {
	base, err := readTree(filepath.Join(baseDir, "canonical"))
	if err != nil {
		return nil, err
	}
	next, err := readTree(filepath.Join(nextDir, "canonical"))
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for k := range base {
		keys[k] = true
	}
	for k := range next {
		keys[k] = true
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	var out []RawDiff
	for _, key := range ordered {
		if string(base[key]) == string(next[key]) {
			continue
		}
		text, truncated := lineDiff(base[key], next[key], 2000)
		out = append(out, RawDiff{File: key, Text: text, Truncated: truncated})
	}
	return out, nil
}

func readTree(root string) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = data
		return nil
	})
	if os.IsNotExist(err) {
		return out, nil
	}
	return out, err
}

func lineDiff(aRaw, bRaw []byte, limit int) (string, bool) {
	a, b := scanLines(aRaw), scanLines(bRaw)
	// Canonical SQLite dumps can contain hundreds of thousands of rows. A
	// quadratic LCS matrix is appropriate for ordinary settings/playlist files,
	// but it must never be allowed to exhaust the server on a large database.
	const maxLCSCells = 4_000_000
	if len(a) > 0 && len(b) > maxLCSCells/len(a) {
		return boundedChangedLines(a, b, limit), true
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
	var out strings.Builder
	i, j, count := 0, 0, 0
	truncated := false
	for i < len(a) || j < len(b) {
		if count >= limit {
			truncated = true
			fmt.Fprintln(&out, "... diff truncated ...")
			break
		}
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			i++
			j++
		case j < len(b) && (i == len(a) || dp[i][j+1] >= dp[i+1][j]):
			fmt.Fprintln(&out, "+"+b[j])
			j++
			count++
		default:
			fmt.Fprintln(&out, "-"+a[i])
			i++
			count++
		}
	}
	return out.String(), truncated
}

func boundedChangedLines(a, b []string, limit int) string {
	var out strings.Builder
	fmt.Fprintln(&out, "... large file: bounded line comparison (LCS skipped) ...")
	max := len(a)
	if len(b) > max {
		max = len(b)
	}
	count := 0
	for i := 0; i < max && count < limit; i++ {
		if i < len(a) && i < len(b) && a[i] == b[i] {
			continue
		}
		if i < len(a) {
			fmt.Fprintln(&out, "-"+a[i])
			count++
		}
		if i < len(b) && count < limit {
			fmt.Fprintln(&out, "+"+b[i])
			count++
		}
	}
	fmt.Fprintln(&out, "... diff truncated ...")
	return out.String()
}
func scanLines(raw []byte) []string {
	s := bufio.NewScanner(strings.NewReader(string(raw)))
	s.Buffer(make([]byte, 1024), 4<<20)
	var out []string
	for s.Scan() {
		out = append(out, s.Text())
	}
	return out
}
