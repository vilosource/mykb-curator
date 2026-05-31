package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestHashFile_DeterministicAndContentSensitive(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := os.WriteFile(p, []byte("hello pipeline"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := hashFile(p)
	if err != nil {
		t.Fatalf("hashFile: %v", err)
	}
	want := sha256.Sum256([]byte("hello pipeline"))
	if got != hex.EncodeToString(want[:]) {
		t.Fatalf("hashFile = %s, want %s", got, hex.EncodeToString(want[:]))
	}

	// Same content → same hash (deterministic).
	if got2, _ := hashFile(p); got != got2 {
		t.Fatalf("hashFile not deterministic: %s != %s", got, got2)
	}

	// Different content → different hash (the property that makes a
	// rebuilt binary self-invalidate the cache).
	if err := os.WriteFile(p, []byte("hello pipeline!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got3, _ := hashFile(p); got3 == got {
		t.Fatal("hashFile insensitive to content change")
	}
}

func TestHashFile_Error(t *testing.T) {
	if _, err := hashFile(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestBuildID_NonEmptyAndStable(t *testing.T) {
	// In a test binary os.Executable() resolves to the test binary, so
	// buildID() takes the executable-hash path and must be non-empty.
	a := buildID()
	if a == "" {
		t.Fatal("buildID returned empty (executable not hashable and no VCS info)")
	}
	if b := buildID(); a != b {
		t.Fatalf("buildID not stable within a process: %s != %s", a, b)
	}
}
