package agenthub

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStaleDisconnectDoesNotDropReplacement(t *testing.T) {
	h := New()
	_, oldDone := h.Connect()
	_, newDone := h.Connect()
	oldDone()
	if !h.Online() {
		t.Fatal("stale connection marked replacement offline")
	}
	newDone()
	if h.Online() {
		t.Fatal("newest disconnect should mark offline")
	}
}

func TestOfflineRequestFailsImmediately(t *testing.T) {
	h := New()
	_, err := h.Request(context.Background(), "/a.mp3", 0, 1023)
	if err == nil || err.Error() != "agent offline" {
		t.Fatalf("offline request err=%v", err)
	}
}

func TestRequestHonorsContextCancellation(t *testing.T) {
	h := New()
	_, done := h.Connect()
	defer done()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.Request(ctx, "/a.mp3", 0, 1023)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("request should stop on context cancellation: %v", err)
	}
}

func TestDeliverCompletesRequest(t *testing.T) {
	h := NewWithTimeout(time.Second)
	requests, done := h.Connect()
	defer done()
	type outcome struct {
		result Result
		err    error
	}
	out := make(chan outcome, 1)
	go func() {
		r, err := h.Request(context.Background(), "/a.mp3", 10, 19)
		out <- outcome{result: r, err: err}
	}()
	req := <-requests
	if req.Path != "/a.mp3" || req.Start != 10 || req.End != 19 {
		t.Fatalf("unexpected request: %+v", req)
	}
	want := Result{Data: []byte("abc"), Start: 10, End: 12, Size: 100}
	if !h.Deliver(req.ID, want) {
		t.Fatal("deliver should find pending request")
	}
	got := <-out
	if got.err != nil || got.result.Start != want.Start || got.result.End != want.End || got.result.Size != want.Size || string(got.result.Data) != "abc" {
		t.Fatalf("unexpected result=%+v err=%v", got.result, got.err)
	}
}

func TestDisconnectFailsPendingRequestImmediately(t *testing.T) {
	h := NewWithTimeout(time.Second)
	requests, done := h.Connect()
	errCh := make(chan error, 1)
	go func() {
		_, err := h.Request(context.Background(), "/a.mp3", 0, 1023)
		errCh <- err
	}()
	<-requests
	done()
	select {
	case err := <-errCh:
		if err == nil || err.Error() != "agent disconnected" {
			t.Fatalf("pending request err=%v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("pending request waited for timeout after Agent disconnect")
	}
}

func TestReplacementFailsOldGenerationPendingRequest(t *testing.T) {
	h := NewWithTimeout(time.Second)
	requests, _ := h.Connect()
	errCh := make(chan error, 1)
	go func() {
		_, err := h.Request(context.Background(), "/a.mp3", 0, 1023)
		errCh <- err
	}()
	<-requests
	_, newDone := h.Connect()
	defer newDone()
	select {
	case err := <-errCh:
		if err == nil || err.Error() != "agent disconnected" {
			t.Fatalf("old generation request err=%v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("old generation request survived Agent replacement")
	}
	if !h.Online() {
		t.Fatal("replacement Agent should remain online")
	}
}

func TestRequestUsesConfiguredTimeout(t *testing.T) {
	h := NewWithTimeout(20 * time.Millisecond)
	requests, done := h.Connect()
	defer done()
	errCh := make(chan error, 1)
	go func() {
		_, err := h.Request(context.Background(), "/a.mp3", 0, 1023)
		errCh <- err
	}()
	<-requests
	select {
	case err := <-errCh:
		if err == nil || err.Error() != "agent timeout" {
			t.Fatalf("timeout err=%v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("configured Agent timeout was not enforced")
	}
}
