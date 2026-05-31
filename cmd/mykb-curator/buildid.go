package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"runtime/debug"
)

// buildID returns a stable identifier for the running binary. It is wired
// into orchestrator.Deps.PipelineVersion, which is mixed into both the IR
// and cluster cache keys. Because a rebuild that changes rendering
// behaviour (prompts, passes, frontends) produces a different binary, the
// ID changes and stale cache entries self-invalidate automatically — no
// manual `rm -rf <cache_dir>/{cluster,ir}` after a pipeline-code change.
// An unchanged rebuild (a reproducible build of the same source) yields
// the same ID, so the cache — and its run-to-run determinism — is
// preserved.
//
// It hashes the executable's own bytes. If that fails (e.g. the executable
// can't be located or read), it falls back to the VCS revision embedded by
// the Go toolchain, then to "" — in which case the orchestrator defaults
// the pipeline version to "v1".
func buildID() string {
	if exe, err := os.Executable(); err == nil {
		if sum, err := hashFile(exe); err == nil {
			return "bin-" + sum[:12]
		}
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		var rev string
		var dirty bool
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if rev != "" {
			if len(rev) > 12 {
				rev = rev[:12]
			}
			if dirty {
				return "vcs-" + rev + "-dirty"
			}
			return "vcs-" + rev
		}
	}
	return ""
}

// hashFile returns the hex-encoded SHA-256 of the file at path.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
