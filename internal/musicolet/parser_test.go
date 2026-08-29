package musicolet

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
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
