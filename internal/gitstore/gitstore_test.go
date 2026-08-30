package gitstore

import (
	"testing"
)

func TestCommitJSON(t *testing.T){s,e:=Open(t.TempDir()+"/r.git");if e!=nil{t.Fatal(e)};c,e:=s.CommitJSON("refs/heads/main","first",[]byte(`{"a":1}`));if e!=nil{t.Fatal(e)};if c==""||s.Head("refs/heads/main")!=c{t.Fatal("missing commit")};b,e:=s.ReadState("refs/heads/main");if e!=nil||string(b)!="{\"a\":1}"{t.Fatalf("state=%q err=%v",b,e)}}

func TestMergeBaseAndConflictIndex(t *testing.T){s,e:=Open(t.TempDir()+"/r.git");if e!=nil{t.Fatal(e)};base,e:=s.CommitJSON("refs/heads/base","base",[]byte(`{"value":"base"}`));if e!=nil{t.Fatal(e)};ours,e:=s.CommitJSON("refs/heads/ours","ours",[]byte(`{"value":"ours"}`),base);if e!=nil{t.Fatal(e)};theirs,e:=s.CommitJSON("refs/heads/theirs","theirs",[]byte(`{"value":"theirs"}`),base);if e!=nil{t.Fatal(e)};mb,e:=s.MergeBase(ours,theirs);if e!=nil||mb!=base{t.Fatalf("merge-base=%q want=%q err=%v",mb,base,e)};conf,e:=s.ConflictIndex(base,ours,theirs);if e!=nil{t.Fatal(e)};if len(conf)!=3{t.Fatalf("conflicts=%#v",conf)};stages:=map[int]bool{};for _,c:=range conf{if c.Path!="state.json"{t.Fatalf("unexpected path %#v",c)};stages[c.Stage]=true};for _,n:=range []int{1,2,3}{if !stages[n]{t.Fatalf("missing stage %d: %#v",n,conf)}}}
