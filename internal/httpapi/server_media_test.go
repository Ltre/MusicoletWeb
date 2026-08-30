package httpapi

import "testing"

func TestParseRange(t *testing.T) {
	r, e := parseRange("")
	if e != nil || r.Requested || r.Start != 0 || r.End != -1 {
		t.Fatalf("%+v %v", r, e)
	}
	r, e = parseRange("bytes=10-19")
	if e != nil || !r.Requested || r.Start != 10 || r.End != 19 {
		t.Fatalf("%+v %v", r, e)
	}
	if _, e = parseRange("bytes=-10"); e == nil {
		t.Fatal("suffix range should be rejected explicitly")
	}
	if _, e = parseRange("bytes=1-2,4-5"); e == nil {
		t.Fatal("multi range should be rejected")
	}
}
