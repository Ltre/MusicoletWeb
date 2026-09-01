package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Ltre/MusicoletWeb/internal/domain"
	"golang.org/x/crypto/blowfish"
	_ "modernc.org/sqlite"
)

const ParserVersion = "musicolet-parser-v1"
const backupKey = "JSTMUSIC_2"

var playCountName = regexp.MustCompile(`^PCs_([YMW])_(.+)$`)

type Result struct {
	Snapshot   domain.Snapshot
	Warnings   []string
	Files      []FileResult
	Validation ValidationResult
}

type ValidationResult struct {
	Status          string   `json:"status"`
	ManifestEntries int      `json:"manifestEntries"`
	Matched         int      `json:"matched"`
	Missing         []string `json:"missing,omitempty"`
	Mismatched      []string `json:"mismatched,omitempty"`
	ManifestHash    string   `json:"manifestHash,omitempty"`
	HashExpected    string   `json:"hashExpected,omitempty"`
	HashVerified    *bool    `json:"hashVerified,omitempty"`
	Notes           []string `json:"notes,omitempty"`
}

type FileResult struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Encrypted  bool   `json:"encrypted"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	MD5        string `json:"md5"`
	ParseState string `json:"parseState"`
}

// ParseArchive extracts without trusting ZIP paths, decrypts each payload and
// generates both a domain snapshot and stable text artifacts for raw diffing.
func ParseArchive(ctx context.Context, archivePath, procedureDir string) (Result, error) {
	result := Result{Snapshot: domain.NewSnapshot()}
	decryptedDir := filepath.Join(procedureDir, "decrypted")
	canonicalDir := filepath.Join(procedureDir, "canonical")
	if err := os.MkdirAll(decryptedDir, 0o700); err != nil {
		return result, err
	}
	if err := os.MkdirAll(canonicalDir, 0o700); err != nil {
		return result, err
	}

	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return result, fmt.Errorf("open backup zip: %w", err)
	}
	defer zr.Close()
	var files []extractedFile
	for _, entry := range zr.File {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		name, err := safeName(entry.Name)
		if err != nil {
			return result, err
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		if entry.UncompressedSize64 > 512<<20 {
			return result, fmt.Errorf("backup entry %q exceeds 512 MiB", name)
		}
		rc, err := entry.Open()
		if err != nil {
			return result, err
		}
		raw, readErr := io.ReadAll(io.LimitReader(rc, 512<<20+1))
		closeErr := rc.Close()
		if readErr != nil {
			return result, readErr
		}
		if closeErr != nil {
			return result, closeErr
		}
		plain, encrypted, err := decodePayload(raw)
		if err != nil {
			return result, fmt.Errorf("decrypt %q: %w", name, err)
		}
		target := filepath.Join(decryptedDir, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return result, err
		}
		if err = os.WriteFile(target, plain, 0o600); err != nil {
			return result, err
		}
		sum, md5sum := sha256.Sum256(plain), md5.Sum(plain)
		result.Files = append(result.Files, FileResult{Name: name, Kind: detectKind(plain), Encrypted: encrypted, Size: int64(len(plain)), SHA256: hex.EncodeToString(sum[:]), MD5: hex.EncodeToString(md5sum[:]), ParseState: "PRESERVED"})
		files = append(files, extractedFile{name: name, path: target})
	}
	if len(files) == 0 {
		return result, errors.New("backup ZIP contains no files")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Name < result.Files[j].Name })
	result.Validation, err = validateBackup(files)
	if err != nil {
		return result, err
	}
	if result.Validation.Status == "UNAVAILABLE" {
		result.Warnings = append(result.Warnings, "backup manifest/hash not recognized; plaintext integrity could not be independently verified")
	}
	if err = writeValidation(procedureDir, result.Validation); err != nil {
		return result, err
	}
	if result.Validation.Status == "FAILED" {
		return result, fmt.Errorf("backup plaintext integrity validation failed: %s", strings.Join(append(result.Validation.Missing, result.Validation.Mismatched...), ", "))
	}

	for i, file := range files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		base := filepath.Base(file.name)
		data, err := os.ReadFile(file.path)
		if err != nil {
			return result, err
		}
		kind := detectKind(data)
		switch {
		case base == "DB_SONGS_LOG" && kind == "sqlite":
			if err = parseSongsDB(ctx, file.path, &result.Snapshot); err != nil {
				return result, fmt.Errorf("parse DB_SONGS_LOG: %w", err)
			}
			result.Files[i].ParseState = "PARSED"
		case strings.HasSuffix(strings.ToLower(base), ".mpl") && kind == "json":
			if err = parsePlaylist(base, data, &result.Snapshot); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("playlist %s: %v", base, err))
			} else {
				result.Files[i].ParseState = "PARSED"
			}
		case base == "0.qstk" && kind == "json":
			if err = parseQueues(data, &result.Snapshot); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("queue: %v", err))
			} else {
				result.Files[i].ParseState = "PARSED"
			}
		case base == "0.favs" && kind == "json":
			if err = parseFavorites(data, &result.Snapshot); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("favorites: %v", err))
			} else {
				result.Files[i].ParseState = "PARSED"
			}
		case playCountName.MatchString(base) && kind == "sqlite":
			if err = parsePlayCounts(ctx, base, file.path, &result.Snapshot); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", base, err))
			} else {
				result.Files[i].ParseState = "PARSED"
			}
		case kind == "json":
			result.Snapshot.Settings[file.name] = json.RawMessage(append([]byte(nil), data...))
			result.Files[i].ParseState = "PRESERVED_JSON"
		case kind == "java-serialization":
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s is Java Serialization and was preserved for a future typed parser", file.name))
		}
		text, err := canonicalText(ctx, file.path, data, kind)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("canonicalize %s: %v", file.name, err))
			continue
		}
		canonPath := filepath.Join(canonicalDir, filepath.FromSlash(file.name)+".txt")
		if err = os.MkdirAll(filepath.Dir(canonPath), 0o700); err != nil {
			return result, err
		}
		if err = os.WriteFile(canonPath, text, 0o600); err != nil {
			return result, err
		}
	}
	materializeReferencedSongs(&result.Snapshot)
	result.Snapshot.Warnings = append([]string(nil), result.Warnings...)
	result.Snapshot.Normalize()
	report, _ := json.MarshalIndent(result.Files, "", "  ")
	if err = os.MkdirAll(filepath.Join(procedureDir, "parser"), 0o700); err != nil {
		return result, err
	}
	if err = os.WriteFile(filepath.Join(procedureDir, "parser", "files.json"), report, 0o600); err != nil {
		return result, err
	}
	canonical, _ := json.MarshalIndent(result.Snapshot, "", "  ")
	if err = os.WriteFile(filepath.Join(procedureDir, "canonical", "snapshot.json"), canonical, 0o600); err != nil {
		return result, err
	}
	return result, nil
}

type extractedFile struct {
	name, path string
}

func writeValidation(procedureDir string, result ValidationResult) error {
	dir := filepath.Join(procedureDir, "parser")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "validation.json"), raw, 0o600)
}

func validateBackup(files []extractedFile) (ValidationResult, error) {
	result := ValidationResult{Status: "UNAVAILABLE"}
	actual := make(map[string]string, len(files))
	var manifest []byte
	var hashFile []byte
	for _, file := range files {
		data, err := os.ReadFile(file.path)
		if err != nil {
			return result, err
		}
		sum := md5.Sum(data)
		actual[filepath.ToSlash(file.name)] = hex.EncodeToString(sum[:])
		switch filepath.Base(file.name) {
		case "0.musicolet.backup":
			manifest = data
		case "hash":
			hashFile = data
		}
	}
	if len(manifest) == 0 {
		result.Notes = append(result.Notes, "0.musicolet.backup is absent")
		return result, nil
	}
	manifestSum := md5.Sum(manifest)
	result.ManifestHash = hex.EncodeToString(manifestSum[:])
	if len(hashFile) > 0 {
		expected := strings.ToLower(strings.TrimSpace(string(hashFile)))
		if len(hashFile) == md5.Size {
			expected = hex.EncodeToString(hashFile)
		}
		result.HashExpected = expected
		ok := expected == result.ManifestHash
		result.HashVerified = &ok
		if !ok {
			result.Status = "FAILED"
			result.Mismatched = append(result.Mismatched, "hash -> 0.musicolet.backup")
		}
	} else {
		result.Notes = append(result.Notes, "hash file is absent")
	}

	expected := manifestMD5s(manifest)
	result.ManifestEntries = len(expected)
	for name, want := range expected {
		name = filepath.ToSlash(strings.TrimPrefix(name, "./"))
		got, ok := actual[name]
		if !ok {
			// Some manifest revisions store only basenames.
			for actualName, actualHash := range actual {
				if filepath.Base(actualName) == filepath.Base(name) {
					got, ok = actualHash, true
					break
				}
			}
		}
		if !ok {
			result.Missing = append(result.Missing, name)
		} else if !strings.EqualFold(got, want) {
			result.Mismatched = append(result.Mismatched, name)
		} else {
			result.Matched++
		}
	}
	sort.Strings(result.Missing)
	sort.Strings(result.Mismatched)
	if len(result.Missing) > 0 || len(result.Mismatched) > 0 {
		result.Status = "FAILED"
	} else if len(expected) > 0 && result.HashVerified != nil && *result.HashVerified {
		result.Status = "VERIFIED"
	} else if len(expected) > 0 || result.HashVerified != nil {
		result.Status = "PARTIAL"
	}
	return result, nil
}

func manifestMD5s(raw []byte) map[string]string {
	out := map[string]string{}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return out
	}
	var visit func(any)
	visit = func(node any) {
		switch current := node.(type) {
		case map[string]any:
			name := firstString(current, "name", "file", "filename", "path")
			sum := firstString(current, "md5", "checksum", "hash")
			if name != "" && isMD5(sum) {
				out[name] = strings.ToLower(sum)
			}
			for key, child := range current {
				if text, ok := child.(string); ok && isMD5(text) && key != "md5" && key != "checksum" && key != "hash" {
					out[key] = strings.ToLower(text)
				}
				visit(child)
			}
		case []any:
			for _, child := range current {
				visit(child)
			}
		}
	}
	visit(value)
	return out
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			return value
		}
	}
	return ""
}

func isMD5(value string) bool {
	if len(value) != md5.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func safeName(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(clean, ":") {
		return "", fmt.Errorf("unsafe ZIP path %q", name)
	}
	return clean, nil
}

func decodePayload(raw []byte) ([]byte, bool, error) {
	if len(raw) == 0 || detectKind(raw) != "binary" {
		return raw, false, nil
	}
	if len(raw)%blowfish.BlockSize != 0 {
		return raw, false, nil
	}
	cipher, err := blowfish.NewCipher([]byte(backupKey))
	if err != nil {
		return nil, false, err
	}
	plain := make([]byte, len(raw))
	for off := 0; off < len(raw); off += blowfish.BlockSize {
		cipher.Decrypt(plain[off:off+blowfish.BlockSize], raw[off:off+blowfish.BlockSize])
	}
	plain, ok := unpad(plain)
	if !ok {
		return raw, false, nil
	}
	if detectKind(plain) == "binary" && !utf8ish(plain) {
		return raw, false, nil
	}
	return plain, true, nil
}

func unpad(data []byte) ([]byte, bool) {
	if len(data) == 0 {
		return data, true
	}
	n := int(data[len(data)-1])
	if n < 1 || n > blowfish.BlockSize || n > len(data) {
		return nil, false
	}
	for _, b := range data[len(data)-n:] {
		if int(b) != n {
			return nil, false
		}
	}
	return data[:len(data)-n], true
}
func utf8ish(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	if !utf8.Valid(data) {
		return false
	}
	printable := 0
	count := 0
	for _, r := range string(data) {
		count++
		if r == 9 || r == 10 || r == 13 || r >= 32 {
			printable++
		}
	}
	return count > 0 && printable*100/count > 85
}
func detectKind(data []byte) string {
	trim := bytes.TrimSpace(data)
	if len(trim) == 0 {
		return "empty"
	}
	if bytes.HasPrefix(trim, []byte("SQLite format 3\x00")) {
		return "sqlite"
	}
	if json.Valid(trim) {
		return "json"
	}
	if len(trim) >= 4 && bytes.Equal(trim[:4], []byte{0xac, 0xed, 0x00, 0x05}) {
		return "java-serialization"
	}
	if utf8ish(trim) {
		return "text"
	}
	return "binary"
}

func openReadOnly(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+url.PathEscape(filepath.ToSlash(path))+"?mode=ro")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func parseSongsDB(ctx context.Context, path string, snap *domain.Snapshot) error {
	db, err := openReadOnly(path)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT * FROM TABLE_SONGS")
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			return err
		}
		m := rowMap(cols, values)
		path := str(m, "COL_PATH")
		if path == "" {
			continue
		}
		song := domain.Song{Path: path, Title: str(m, "COL_TITLE"), Artist: str(m, "COL_ARTIST"), Album: str(m, "COL_ALBUM"), AlbumArtist: firstStr(m, "COL_ALBUM_ARTIST", "COL_ALBUMARTIST"), Composer: str(m, "COL_COMPOSER"), Genre: str(m, "COL_GENRE"), Lyrics: str(m, "COL_LYRICS"), Comment: firstStr(m, "COL_COMMENT", "COL_COMMENTS"), TrackNo: str(m, "COL_TRACK_NO"), DiscNo: firstStr(m, "COL_DISC_NO", "COL_DISC_NUMBER"), Year: num(m, "COL_YEAR"), DurationMS: num(m, "COL_DURATION"), DateAddedMS: num(m, "COL_DATE_ADDED"), DateModifiedMS: num(m, "COL_DATE_MODIFIED"), Bitrate: num(m, "COL_BITRATE"), SampleRate: num(m, "COL_SAMPLE_RATE"), BitDepth: num(m, "COL_BITS_PER_SAMPLE"), Format: str(m, "COL_FORMAT"), Codec: str(m, "COL_CODEC"), Channels: num(m, "COL_CHANNELS"), FileSize: firstNum(m, "COL_FILE_SIZE", "COL_SIZE")}
		snap.Songs[path] = song
		stat := domain.PlaybackStats{Path: path, Total: num(m, "COL_NUM_PLAYED"), LastPlayed: secondsTimestamp(num(m, "COL_LAST_PLAYED")), Weekly: map[string]int64{"current": num(m, "COL_NUM_PLAYED_W")}, Monthly: map[string]int64{"current": num(m, "COL_NUM_PLAYED_M")}, Yearly: map[string]int64{"current": num(m, "COL_NUM_PLAYED_Y")}}
		snap.Stats[path] = stat
	}
	return rows.Err()
}

func parsePlaylist(name string, data []byte, snap *domain.Snapshot) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	paths := stringsArray(raw["S_P"])
	title := strings.TrimSuffix(name, filepath.Ext(name))
	snap.Playlists = append(snap.Playlists, domain.OrderedList{SourceKey: "musicolet:playlist:" + name, Name: title, Paths: paths})
	mergeSongHints(raw, snap)
	return nil
}
func parseFavorites(data []byte, snap *domain.Snapshot) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, path := range stringsArray(raw["S_P"]) {
		song := snap.Songs[path]
		song.Path = path
		song.Favorite = true
		snap.Songs[path] = song
	}
	mergeSongHints(raw, snap)
	return nil
}
func parseQueues(data []byte, snap *domain.Snapshot) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	snap.CurrentQueueIndex = int(toInt(raw["S0_CPQ"]))
	items, _ := raw["S0_PQ"].([]any)
	for i, item := range items {
		obj, _ := item.(map[string]any)
		songsObj, _ := obj["S0_PQ_OQS"].(map[string]any)
		paths := stringsArray(songsObj["S_P"])
		name := toString(obj["S0_PQ_T"])
		if name == "" {
			name = fmt.Sprintf("Queue %d", i+1)
		}
		snap.Queues = append(snap.Queues, domain.Queue{OrderedList: domain.OrderedList{SourceKey: fmt.Sprintf("musicolet:queue:%d", i), Name: name, Paths: paths}, Position: i, Current: int(toInt(obj["S0_PQ_CPS"])), ProgressMS: toInt(obj["S0_PQ_LKP"])})
		mergeSongHints(songsObj, snap)
	}
	return nil
}

func mergeSongHints(raw map[string]any, snap *domain.Snapshot) {
	paths := stringsArray(raw["S_P"])
	titles := stringsArray(raw["S_T"])
	albums := stringsArray(raw["S_AL"])
	artists := stringsArray(raw["S_AR"])
	durations := intArray(raw["S_D"])
	for i, path := range paths {
		song := snap.Songs[path]
		song.Path = path
		if song.Title == "" && i < len(titles) {
			song.Title = titles[i]
		}
		if song.Album == "" && i < len(albums) {
			song.Album = albums[i]
		}
		if song.Artist == "" && i < len(artists) {
			song.Artist = artists[i]
		}
		if song.DurationMS == 0 && i < len(durations) {
			song.DurationMS = durations[i]
		}
		snap.Songs[path] = song
	}
}

func parsePlayCounts(ctx context.Context, name, path string, snap *domain.Snapshot) error {
	m := playCountName.FindStringSubmatch(name)
	if len(m) != 3 {
		return nil
	}
	db, err := openReadOnly(path)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT COL_PATH,COL_NUM_PLAYED FROM TABLE_SONGS")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var path string
		var count int64
		if err = rows.Scan(&path, &count); err != nil {
			return err
		}
		stat := snap.Stats[path]
		stat.Path = path
		if stat.Weekly == nil {
			stat.Weekly = map[string]int64{}
		}
		if stat.Monthly == nil {
			stat.Monthly = map[string]int64{}
		}
		if stat.Yearly == nil {
			stat.Yearly = map[string]int64{}
		}
		switch m[1] {
		case "W":
			stat.Weekly[m[2]] = count
		case "M":
			stat.Monthly[m[2]] = count
		case "Y":
			stat.Yearly[m[2]] = count
		}
		snap.Stats[path] = stat
	}
	return rows.Err()
}

func materializeReferencedSongs(snap *domain.Snapshot) {
	for _, list := range snap.Playlists {
		for _, path := range list.Paths {
			if _, ok := snap.Songs[path]; !ok {
				snap.Songs[path] = domain.Song{Path: path, Title: filepath.Base(path)}
			}
		}
	}
	for _, queue := range snap.Queues {
		for _, path := range queue.Paths {
			if _, ok := snap.Songs[path]; !ok {
				snap.Songs[path] = domain.Song{Path: path, Title: filepath.Base(path)}
			}
		}
	}
}

func canonicalText(ctx context.Context, path string, data []byte, kind string) ([]byte, error) {
	switch kind {
	case "json":
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, err
		}
		return json.MarshalIndent(value, "", "  ")
	case "sqlite":
		return dumpSQLite(ctx, path)
	case "text", "empty":
		return data, nil
	default:
		return []byte("# preserved binary\nsha256=" + hash(data) + "\nsize=" + strconv.Itoa(len(data)) + "\n"), nil
	}
}

func dumpSQLite(ctx context.Context, path string) ([]byte, error) {
	db, err := openReadOnly(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tables, err := db.QueryContext(ctx, "SELECT name,sql FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer tables.Close()
	type table struct{ name, ddl string }
	var list []table
	for tables.Next() {
		var t table
		if err = tables.Scan(&t.name, &t.ddl); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	var out bytes.Buffer
	for _, t := range list {
		fmt.Fprintf(&out, "TABLE %s\nSCHEMA %s\n", t.name, t.ddl)
		rows, err := db.QueryContext(ctx, "SELECT * FROM \""+strings.ReplaceAll(t.name, "\"", "\"\"")+"\"")
		if err != nil {
			return nil, err
		}
		cols, _ := rows.Columns()
		var records []string
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err = rows.Scan(ptrs...); err != nil {
				rows.Close()
				return nil, err
			}
			parts := make([]string, len(cols))
			for i := range cols {
				parts[i] = cols[i] + "=" + toString(vals[i])
			}
			records = append(records, strings.Join(parts, "\t"))
		}
		rows.Close()
		sort.Strings(records)
		for _, record := range records {
			out.WriteString(record)
			out.WriteByte('\n')
		}
	}
	return out.Bytes(), nil
}

func rowMap(cols []string, values []any) map[string]any {
	m := make(map[string]any, len(cols))
	for i, col := range cols {
		m[strings.ToUpper(col)] = values[i]
	}
	return m
}
func firstStr(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v := str(m, key); v != "" {
			return v
		}
	}
	return ""
}
func firstNum(m map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if v := num(m, key); v != 0 {
			return v
		}
	}
	return 0
}
func str(m map[string]any, key string) string { return toString(m[strings.ToUpper(key)]) }
func num(m map[string]any, key string) int64  { return toInt(m[strings.ToUpper(key)]) }
func toString(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case []byte:
		return string(value)
	case json.Number:
		return value.String()
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(value, 10)
	default:
		return fmt.Sprint(value)
	}
}
func toInt(v any) int64 {
	switch value := v.(type) {
	case nil:
		return 0
	case int64:
		return value
	case float64:
		return int64(value)
	case json.Number:
		n, _ := value.Int64()
		return n
	case []byte:
		n, _ := strconv.ParseInt(string(value), 10, 64)
		return n
	case string:
		n, _ := strconv.ParseInt(value, 10, 64)
		return n
	default:
		n, _ := strconv.ParseInt(fmt.Sprint(value), 10, 64)
		return n
	}
}

func secondsTimestamp(value int64) int64 {
	// Musicolet currently stores COL_LAST_PLAYED in Unix milliseconds. The
	// product decision is explicitly second precision, so discard rather than
	// preserve sub-second detail. The guard keeps older second-based exports.
	if value > 10_000_000_000 {
		return value / 1000
	}
	return value
}
func stringsArray(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, toString(item))
	}
	return out
}
func intArray(v any) []int64 {
	items, _ := v.([]any)
	out := make([]int64, 0, len(items))
	for _, item := range items {
		out = append(out, toInt(item))
	}
	return out
}
func hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
