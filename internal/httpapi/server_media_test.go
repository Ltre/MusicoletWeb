package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Ltre/MusicoletWeb/internal/agenthub"
)

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

func TestServeMediaStreamsWholeFileAcrossAgentChunks(t *testing.T) {
	data := make([]byte, mediaChunk+37)
	for i := range data {
		data[i] = byte(i % 251)
	}
	h := agenthub.NewWithTimeout(time.Second)
	requests, done := h.Connect()
	defer done()
	served := make(chan int, 1)
	go func() {
		count := 0
		for count < 2 {
			req := <-requests
			end := req.End
			if end >= int64(len(data)) {
				end = int64(len(data)) - 1
			}
			chunk := append([]byte(nil), data[req.Start:end+1]...)
			h.Deliver(req.ID, agenthub.Result{Data: chunk, Start: req.Start, End: end, Size: int64(len(data))})
			count++
		}
		served <- count
	}()

	s := &Server{Hub: h}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/media", nil)
	s.serveMedia(rr, req, "/music/test.bin")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Length"); got != strconv.Itoa(len(data)) {
		t.Fatalf("Content-Length=%q want=%d", got, len(data))
	}
	if !bytes.Equal(rr.Body.Bytes(), data) {
		t.Fatalf("full media body mismatch: got=%d want=%d", rr.Body.Len(), len(data))
	}
	select {
	case n := <-served:
		if n != 2 {
			t.Fatalf("Agent request count=%d want=2", n)
		}
	case <-time.After(time.Second):
		t.Fatal("Agent did not receive both media chunks")
	}
}

func TestServeMediaRangeReturns206(t *testing.T) {
	data := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	h := agenthub.NewWithTimeout(time.Second)
	requests, done := h.Connect()
	defer done()
	go func() {
		req := <-requests
		end := req.End
		if end >= int64(len(data)) {
			end = int64(len(data)) - 1
		}
		h.Deliver(req.ID, agenthub.Result{Data: append([]byte(nil), data[req.Start:end+1]...), Start: req.Start, End: end, Size: int64(len(data))})
	}()

	s := &Server{Hub: h}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/media", nil)
	req.Header.Set("Range", "bytes=5-12")
	s.serveMedia(rr, req, "/music/test.bin")
	if rr.Code != http.StatusPartialContent {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Range"); got != "bytes 5-12/36" {
		t.Fatalf("Content-Range=%q", got)
	}
	if got := rr.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("Accept-Ranges=%q", got)
	}
	if got := rr.Body.String(); got != "56789abc" {
		t.Fatalf("range body=%q", got)
	}
}

func TestServeMediaOfflineReturns503Immediately(t *testing.T) {
	h := agenthub.NewWithTimeout(time.Second)
	s := &Server{Hub: h}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/media", nil)
	started := time.Now()
	s.serveMedia(rr, req, "/music/test.bin")
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "agent offline") {
		t.Fatalf("offline body=%q", rr.Body.String())
	}
	if time.Since(started) > 250*time.Millisecond {
		t.Fatal("offline media request should fail immediately")
	}
}
