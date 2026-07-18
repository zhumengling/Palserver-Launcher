package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvedPathWithinAllowedRootsRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "managed")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symbolic links are unavailable on this test host: %v", err)
	}
	allowed, err := resolvedPathWithinAllowedRoots(filepath.Join(link, "not-created", "server"), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("resolved containment accepted a non-existent child below an escaping symlink")
	}
}

func TestResolvedPathWithinAllowedRootsAllowsNonexistentChild(t *testing.T) {
	root := filepath.Join(t.TempDir(), "managed")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	allowed, err := resolvedPathWithinAllowedRoots(filepath.Join(root, "not-created", "server"), []string{root})
	if err != nil || !allowed {
		t.Fatalf("non-existent managed child allowed=%v err=%v", allowed, err)
	}
}
