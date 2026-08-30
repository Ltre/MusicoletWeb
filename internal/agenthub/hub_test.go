package agenthub

import (
	"context"
	"errors"
	"testing"
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
