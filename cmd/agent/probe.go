package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	flag.Func("probe", "read one byte from a song path/URI and exit (Termux diagnostics)", func(source string) error {
		roots := probeRoots(env("MUSICOLET_AGENT_ROOTS", "/storage/emulated/0"))
		if len(roots) == 0 {
			return fmt.Errorf("no readable roots")
		}
		r, err := readRange(source, 0, 0, roots)
		if err != nil {
			return err
		}
		fmt.Printf("OK size=%d first-byte=%02x source=%s\n", r.Size, r.Data[0], source)
		os.Exit(0)
		return nil
	})
}

func probeRoots(raw string) []string {
	roots := []string{}
	for _, root := range strings.Split(raw, ",") {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if real, err := filepath.EvalSymlinks(root); err == nil {
			roots = append(roots, filepath.Clean(real))
		}
	}
	return roots
}
