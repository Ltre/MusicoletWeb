package agentclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlainHTTPRequiresExplicitDevelopmentOptIn(t *testing.T) {
	err := runConnection(context.Background(), Config{ServerURL: "http://127.0.0.1:1", Token: "test"}, []string{t.TempDir()}, t.Logf)
	if err == nil || !strings.Contains(err.Error(), "plain HTTP") {
		t.Fatalf("err=%v", err)
	}
}

func TestReadOnlyResolverRejectsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.mp3")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	roots, err := normalizeRoots([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = resolve(outside, roots); err == nil {
		t.Fatal("outside path was accepted")
	}
}
