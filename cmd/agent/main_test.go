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

func TestMusicoletMediaStoreCandidates(t *testing.T) {
	in := "musicolet://media-store?p_v=primary&p_rp=1%2Fytdl%2F%E6%BD%AE%E5%B7%9E%E6%AD%8C&p_dn=%E8%80%81%E5%8E%9D.m4a&p_id=1000603564&p_mt=1"
	got := musicoletMediaStoreCandidates(in)
	want := []string{
		"content://media/external_primary/audio/media/1000603564",
		"content://media/external/audio/media/1000603564",
	}
	if len(got) != len(want) {
		t.Fatalf("candidates=%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestMusicoletMediaStoreCandidatesRejectUnsafe(t *testing.T) {
	bad := []string{
		"musicolet://media-store?p_v=primary&p_id=12&p_mt=2",
		"musicolet://media-store?p_v=../../etc&p_id=12&p_mt=1",
		"musicolet://media-store?p_v=primary&p_id=12%2F34&p_mt=1",
		"musicolet://media-store?p_v=primary&p_id=12&p_mt=1&evil=x",
		"musicolet://other?p_v=primary&p_id=12&p_mt=1",
	}
	for _, in := range bad {
		if got := musicoletMediaStoreCandidates(in); len(got) != 0 {
			t.Fatalf("unsafe URI accepted: %q => %v", in, got)
		}
	}
}
