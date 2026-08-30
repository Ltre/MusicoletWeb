package agenthub

import "testing"

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
