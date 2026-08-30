package app

import (
	"encoding/json"
	"testing"
)

func TestOrderedResolutionPatch(t *testing.T){p:=buildResolutionPatch("queue",json.RawMessage(`["A","B","C"]`),json.RawMessage(`["A","C","B"]`),json.RawMessage(`["C","A","D"]`));var v map[string]any;if json.Unmarshal(p,&v)!=nil{t.Fatal(string(p))};ops:=v["from_ours"].([]any);if len(ops)==0{t.Fatal("expected detailed operations")}}
