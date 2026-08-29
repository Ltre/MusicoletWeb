package musicolet

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/Ltre/MusicoletWeb/internal/domain"
	"golang.org/x/crypto/blowfish"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const Key = "JSTMUSIC_2"

type Parser struct{}

func Decrypt(data []byte) ([]byte, error) {
	if len(data)%blowfish.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext length %d not multiple of block size", len(data))
	}
	c, err := blowfish.NewCipher([]byte(Key))
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += blowfish.BlockSize {
		c.Decrypt(out[i:i+blowfish.BlockSize], data[i:i+blowfish.BlockSize])
	}
	if len(out) == 0 {
		return out, nil
	}
	p := int(out[len(out)-1])
	if p < 1 || p > blowfish.BlockSize || p > len(out) {
		return nil, fmt.Errorf("bad padding")
	}
	for _, b := range out[len(out)-p:] {
		if int(b) != p {
			return nil, fmt.Errorf("bad padding")
		}
	}
	return out[:len(out)-p], nil
}

func (Parser) ParseZip(ctx context.Context, zipPath, workDir string) (domain.Snapshot, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return domain.Snapshot{}, err
	}
	defer zr.Close()
	snap := domain.EmptySnapshot()
	decDir := filepath.Join(workDir, "decrypted")
	_ = os.MkdirAll(decDir, 0o700)
	files := map[string][]byte{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, e := f.Open()
		if e != nil {
			return snap, e
		}
		b, e := io.ReadAll(rc)
		rc.Close()
		if e != nil {
			return snap, e
		}
		plain, e := Decrypt(b)
		if e != nil {
			plain = b
		}
		files[f.Name] = plain
	}
	if manifest, ok := files["0.musicolet.backup"]; ok {
		if hv, ok := files["hash"]; ok {
			sum := md5.Sum(manifest)
			hexsum := hex.EncodeToString(sum[:])
			trimmed := strings.TrimSpace(string(hv))
			valid := bytes.Equal(hv, sum[:]) || strings.EqualFold(trimmed, hexsum)
			if !valid {
				return snap, fmt.Errorf("backup manifest hash mismatch")
			}
		}
	}
	for name, plain := range files {
		safe := filepath.Base(name)
		_ = os.WriteFile(filepath.Join(decDir, safe), plain, 0o600)
		if bytes.HasPrefix(plain, []byte("SQLite format 3")) {
			if txt, e := canonicalSQLite(ctx, plain); e == nil {
				snap.RawFiles[name] = txt
			} else {
				snap.RawFiles[name] = canonicalText(plain)
			}
		} else {
			snap.RawFiles[name] = canonicalText(plain)
		}
		switch {
		case name == "DB_SONGS_LOG":
			if e := parseSongsDB(ctx, plain, &snap); e != nil {
				return snap, fmt.Errorf("DB_SONGS_LOG: %w", e)
			}
		case strings.HasPrefix(name, "PCs_"):
			if len(plain) > 0 && bytes.HasPrefix(plain, []byte("SQLite format 3")) {
				if e := parseCountsDB(ctx, plain, name, &snap); e != nil {
					return snap, fmt.Errorf("%s: %w", name, e)
				}
			}
		case strings.HasSuffix(name, ".mpl"):
			if e := parsePlaylistJSON(plain, strings.TrimSuffix(filepath.Base(name), ".mpl"), &snap); e != nil {
				return snap, fmt.Errorf("%s: %w", name, e)
			}
		case name == "0.qstk":
			if e := parseQueuesJSON(plain, &snap); e != nil {
				return snap, fmt.Errorf("0.qstk: %w", e)
			}
		case name == "0.favs":
			if e := parseFavoritesJSON(plain, &snap); e != nil {
				return snap, fmt.Errorf("0.favs: %w", e)
			}
		}
	}
	return snap, nil
}

func canonicalText(b []byte) string {
	if json.Valid(b) {
		var v any
		if json.Unmarshal(b, &v) == nil {
			c, _ := json.MarshalIndent(v, "", "  ")
			return string(c)
		}
	}
	if bytes.IndexByte(b, 0) < 0 {
		return string(b)
	}
	return "binary-hex:\n" + hex.Dump(b)
}

func canonicalSQLite(ctx context.Context, data []byte) (string, error) {
	p, e := tempDB(data)
	if e != nil {
		return "", e
	}
	defer os.Remove(p)
	d, e := sql.Open("sqlite", "file:"+p+"?mode=ro")
	if e != nil {
		return "", e
	}
	defer d.Close()
	rows, e := d.QueryContext(ctx, "SELECT type,name,COALESCE(sql,'') FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name")
	if e != nil {
		return "", e
	}
	type obj struct{ typ, name, sql string }
	var objs []obj
	for rows.Next() {
		var o obj
		if e = rows.Scan(&o.typ, &o.name, &o.sql); e != nil {
			rows.Close()
			return "", e
		}
		objs = append(objs, o)
	}
	rows.Close()
	var out strings.Builder
	for _, o := range objs {
		fmt.Fprintf(&out, "-- %s %s\n%s;\n", o.typ, o.name, o.sql)
		if o.typ != "table" {
			continue
		}
		q := "SELECT * FROM " + quoteIdent(o.name)
		rr, e := d.QueryContext(ctx, q)
		if e != nil {
			return "", e
		}
		cols, _ := rr.Columns()
		var lines []string
		for rr.Next() {
			vals := make([]any, len(cols))
			ptr := make([]any, len(cols))
			for i := range vals {
				ptr[i] = &vals[i]
			}
			if e = rr.Scan(ptr...); e != nil {
				rr.Close()
				return "", e
			}
			m := map[string]any{}
			for i, c := range cols {
				switch v := vals[i].(type) {
				case []byte:
					m[c] = string(v)
				default:
					m[c] = v
				}
			}
			b, _ := json.Marshal(m)
			lines = append(lines, string(b))
		}
		rr.Close()
		sort.Strings(lines)
		for _, line := range lines {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	return out.String(), nil
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func tempDB(data []byte) (string, error) {
	f, e := os.CreateTemp("", "musicolet-*.db")
	if e != nil {
		return "", e
	}
	p := f.Name()
	if _, e = f.Write(data); e != nil {
		f.Close()
		os.Remove(p)
		return "", e
	}
	f.Close()
	return p, nil
}
