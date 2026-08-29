package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecurePathAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "a.mp3")
	if e := os.WriteFile(inside, []byte("x"), 0600); e != nil {
		t.Fatal(e)
	}
	real, _ := filepath.EvalSymlinks(root)
	if _, e := securePath(inside, []string{real}); e != nil {
		t.Fatal(e)
	}
	outside := filepath.Join(t.TempDir(), "x.mp3")
	_ = os.WriteFile(outside, []byte("x"), 0600)
	link := filepath.Join(root, "link.mp3")
	if e := os.Symlink(outside, link); e != nil {
		t.Skip("symlink unavailable")
	}
	if _, e := securePath(link, []string{real}); e == nil {
		t.Fatal("symlink escape accepted")
	}
}
func TestResolveExternalStorageURI(t *testing.T) {
	p, e := resolvePath("content://com.android.externalstorage.documents/document/primary%3AMusic%2Fa.mp3")
	if e != nil {
		t.Fatal(e)
	}
	if p != "/storage/emulated/0/Music/a.mp3" {
		t.Fatalf("%q", p)
	}
}
