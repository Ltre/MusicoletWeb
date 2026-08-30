//go:build integration

package musicolet

import (
	"archive/zip"
	"context"
	"database/sql"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"testing"
)

func TestSongsSQLiteFixture(t *testing.T){dir:=t.TempDir();dbp:=filepath.Join(dir,"songs.db");d,e:=sql.Open("sqlite",dbp);if e!=nil{t.Fatal(e)};_,e=d.Exec(`CREATE TABLE TABLE_SONGS(COL_PATH TEXT PRIMARY KEY,COL_TITLE TEXT,COL_ARTIST TEXT,COL_NUM_PLAYED INTEGER,COL_LAST_PLAYED INTEGER); INSERT INTO TABLE_SONGS VALUES('/a.mp3','A','X',7,1700000000123)`);if e!=nil{t.Fatal(e)};d.Close();raw,e:=os.ReadFile(dbp);if e!=nil{t.Fatal(e)};zp:=filepath.Join(dir,"b.zip");f,_:=os.Create(zp);z:=zip.NewWriter(f);w,_:=z.Create("DB_SONGS_LOG");_,_=w.Write(raw);_=z.Close();_=f.Close();s,e:=(Parser{}).ParseZip(context.Background(),zp,dir);if e!=nil{t.Fatal(e)};x:=s.Songs["/a.mp3"];if x.Title!="A"||x.PlayCount!=7||x.LastPlayedMS!=1700000000000{t.Fatalf("%+v",x)}}
