package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ltre/MusicoletWeb/internal/agenthub"
)

func TestServeMediaRejectsMalformedFirstAgentChunk(t *testing.T) {
	h := agenthub.NewWithTimeout(time.Second)
	requests, done := h.Connect()
	defer done()
	go func() {
		req := <-requests
		// Metadata claims four bytes, but the body only contains two.
		h.Deliver(req.ID, agenthub.Result{Data: []byte{1, 2}, Start: req.Start, End: req.Start + 3, Size: 10})
	}()

	s := &Server{Hub: h}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/media", nil)
	s.serveMedia(rr, req, "/music/test.bin")
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "invalid Agent media chunk") {
		t.Fatalf("body=%q", rr.Body.String())
	}
}

func TestValidAgentChunkRequiresExactRangeBodyAndStableSize(t *testing.T) {
	good := agenthub.Result{Data: []byte{1, 2, 3}, Start: 5, End: 7, Size: 20}
	if !validAgentChunk(good, 5, 9, 20) {
		t.Fatal("valid chunk rejected")
	}
	badStart := good
	badStart.Start = 4
	if validAgentChunk(badStart, 5, 9, 20) {
		t.Fatal("wrong start accepted")
	}
	badBody := good
	badBody.Data = []byte{1, 2}
	if validAgentChunk(badBody, 5, 9, 20) {
		t.Fatal("short body accepted")
	}
	badSize := good
	badSize.Size = 21
	if validAgentChunk(badSize, 5, 9, 20) {
		t.Fatal("changed file size accepted")
	}
}
