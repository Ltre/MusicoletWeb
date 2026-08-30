package musicolet

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"golang.org/x/crypto/blowfish"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePlainJSONFixtures(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "b.zip")
	f, e := os.Create(p)
	if e != nil {
		t.Fatal(e)
	}
	z := zip.NewWriter(f)
	write := func(n, s string) { w, _ := z.Create(n); w.Write([]byte(s)) }
	write("Mix.mpl", `{"S_P":["/a.mp3","/b.mp3","/a.mp3"]}`)
	write("0.favs", `{"S_P":["/b.mp3"]}`)
	write("0.qstk", `{"S0_PQ":[{"S0_PQ_T":"Q","S0_PQ_CPS":1,"S0_PQ_LKP":12000,"S0_PQ_OQS":{"S_P":["/a.mp3","/b.mp3"]}}]}`)
	z.Close()
	f.Close()
	s, e := (Parser{}).ParseZip(context.Background(), p, dir)
	if e != nil {
		t.Fatal(e)
	}
	if len(s.Playlists) != 1 || len(s.Playlists[0].Paths) != 2 {
		t.Fatalf("playlist %#v", s.Playlists)
	}
	if !s.Favorites["/b.mp3"] {
		t.Fatal("favorite missing")
	}
	if len(s.Queues) != 1 || s.Queues[0].CurrentIndex != 1 || s.Queues[0].PositionMS != 12000 {
		t.Fatalf("queue %#v", s.Queues)
	}
	if !bytes.Contains(CanonicalSnapshot(s), []byte(`"paths"`)) {
		t.Fatal("canonical missing")
	}
}

func TestManifestPerFileMD5AndSettings(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "b.zip")
	f, _ := os.Create(p)
	z := zip.NewWriter(f)
	fav := []byte(`{"S_P":["/b.mp3"]}`)
	sum := md5.Sum(fav)
	manifest := []byte(fmt.Sprintf(`{"files":{"0.favs":"%x"}}`, sum))
	mh := md5.Sum(manifest)
	for n, b := range map[string][]byte{"0.favs": fav, "0.musicolet.backup": manifest, "hash": []byte(hex.EncodeToString(mh[:])), "0.settings": []byte(`{"theme":"dark"}`)} {
		w, _ := z.Create(n)
		_, _ = w.Write(b)
	}
	_ = z.Close()
	_ = f.Close()
	s, r, e := (Parser{}).ParseZipWithReport(context.Background(), p, dir)
	if e != nil {
		t.Fatal(e)
	}
	if r.ManifestEntries != 1 || r.ManifestValidated != 1 {
		t.Fatalf("report %#v", r)
	}
	if string(s.Settings["0.settings"]) != `{"theme":"dark"}` {
		t.Fatalf("settings %s", s.Settings["0.settings"])
	}
}

func TestJavaSerializationCanonical(t *testing.T) {
	b := []byte{0xac, 0xed, 0x00, 0x05, 0x74, 0x00, 0x03, 'a', 'b', 'c'}
	x, e := canonicalJavaSerialization(b)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(x, `TC_STRING "abc"`) {
		t.Fatal(x)
	}
}

func TestEncryptedFixture(t *testing.T) {
	plain := []byte(`{"S_P":["/x.mp3"]}`)
	enc := encryptFixture(t, plain)
	dir := t.TempDir()
	p := filepath.Join(dir, "e.zip")
	f, _ := os.Create(p)
	z := zip.NewWriter(f)
	w, _ := z.Create("0.favs")
	_, _ = w.Write(enc)
	_ = z.Close()
	_ = f.Close()
	s, r, e := (Parser{}).ParseZipWithReport(context.Background(), p, dir)
	if e != nil {
		t.Fatal(e)
	}
	if !s.Favorites["/x.mp3"] || r.Decrypted != 1 {
		t.Fatalf("snapshot/report %#v %#v", s, r)
	}
}

func TestSafeArchivePath(t *testing.T) {
	good, err := safeArchivePath("nested/playlist/file.json")
	if err != nil || filepath.ToSlash(good) != "nested/playlist/file.json" {
		t.Fatalf("good=%q err=%v", good, err)
	}
	for _, bad := range []string{"../escape", "/absolute", "a/../../escape", `..\escape`} {
		if _, err := safeArchivePath(bad); err == nil {
			t.Fatalf("unsafe path accepted: %q", bad)
		}
	}
}

func TestDuplicateBackupEntryRejected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "dup.zip")
	f, _ := os.Create(p)
	z := zip.NewWriter(f)
	for i := 0; i < 2; i++ {
		w, _ := z.Create("0.favs")
		_, _ = w.Write([]byte(`{"S_P":[]}`))
	}
	_ = z.Close()
	_ = f.Close()
	if _, _, err := (Parser{}).ParseZipWithReport(context.Background(), p, dir); err == nil {
		t.Fatal("duplicate entry must be rejected")
	}
}

func encryptFixture(t *testing.T, plain []byte) []byte {
	c, e := blowfish.NewCipher([]byte(Key))
	if e != nil {
		t.Fatal(e)
	}
	pad := blowfish.BlockSize - len(plain)%blowfish.BlockSize
	b := append([]byte(nil), plain...)
	b = append(b, bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(b))
	for i := 0; i < len(b); i += blowfish.BlockSize {
		c.Encrypt(out[i:i+blowfish.BlockSize], b[i:i+blowfish.BlockSize])
	}
	return out
}

func TestSongFromRealBackupColumns(t *testing.T) {
	m := map[string]any{
		"COL_PATH":          "content://com.android.externalstorage.documents/tree/primary%3AMusic/document/primary%3AMusic%2FAlbum%2Fsong.mp3",
		"COL_LOGPATH":       "Storage/primary/Music/Album/song.mp3",
		"COL_TITLE":         "Song",
		"COL_ARTIST":        "Artist",
		"COL_ALBUM":         "Album",
		"album_artist":      "Album Artist",
		"COL_COMPOSER":      "Composer",
		"COL_GENRE":         "Genre",
		"COL_YEAR":          int64(2026),
		"COL_DURATION":      int64(211148),
		"COL_TRACK_NO":      int64(2005),
		"COL_DATE_ADDED":    int64(1689671413000),
		"COL_DATE_MODIFIED": int64(1670240617000),
		"COL_NUM_PLAYED":    int64(3),
		"COL_LAST_PLAYED":   int64(1690099781718),
	}
	s := songFromDBRow(m)
	if s.AlbumArtist != "Album Artist" || s.TrackNo != "5" || s.DiscNo != "2" {
		t.Fatalf("metadata aliases/track decode failed: %#v", s)
	}
	if s.FileName != "song.mp3" || filepath.ToSlash(s.Folder) != "Storage/primary/Music/Album" {
		t.Fatalf("COL_LOGPATH presentation mapping failed: file=%q folder=%q", s.FileName, s.Folder)
	}
	if s.Path != m["COL_PATH"] {
		t.Fatalf("source path must remain unchanged: %q", s.Path)
	}
}
