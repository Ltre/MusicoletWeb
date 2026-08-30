package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecurePathRejectsEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "song.mp3")
	if err := os.WriteFile(target, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape.mp3")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	if _, err := securePath(link, []string{root}); err == nil {
		t.Fatal("expected symlink escape rejection")
	}
}
func TestReadRangeFile(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.mp3")
	if err := os.WriteFile(p, []byte("0123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := readRange(p, 2, 5, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Data) != "2345" || r.Start != 2 || r.End != 5 || r.Size != 10 {
		t.Fatalf("%+v %q", r, string(r.Data))
	}
}
func TestMediaURIRestriction(t *testing.T) {
	if !mediaURI.MatchString("content://media/external/audio/media/123") {
		t.Fatal("expected audio media URI")
	}
	if mediaURI.MatchString("content://media/external/images/media/123") {
		t.Fatal("must not allow images provider")
	}
	if mediaURI.MatchString("content://settings/system/foo") {
		t.Fatal("must not allow arbitrary provider")
	}
}
