package specchat

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Hermetic: a stub `kb` script records its argv and prints an id.
// Proves AddEntry passes exactly the D6 contract args and parses the
// id, and that a non-zero exit surfaces as an error (entry not made).
func TestShellKBWriter_AddEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX")
	}
	dir := t.TempDir()
	argLog := filepath.Join(dir, "argv")
	stub := filepath.Join(dir, "kb")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argLog + "\necho 'added [Hv17Cur9] to vault'\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	w := &ShellKBWriter{Bin: stub}
	id, err := w.AddEntry(context.Background(), "vault", "fact",
		"daily Raft snapshot -> GRS", "hashicorp-vault runbook", "closes Judge gap")
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if id != "Hv17Cur9" {
		t.Fatalf("parsed id = %q, want Hv17Cur9", id)
	}
	got, _ := os.ReadFile(argLog)
	argv := strings.Split(strings.TrimSpace(string(got)), "\n")
	// Agent/curator-proposed entries are quarantined into kb's
	// 'incoming' zone (mykb#30) — never straight into active. The
	// --zone incoming arg is the contract, asserted on the real argv
	// (not a self-scripted stub) so curatorapi's reported zone can't
	// drift from what is actually written (mykb-curator#2).
	want := []string{"add", "fact", "vault", "daily Raft snapshot -> GRS",
		"--source", "hashicorp-vault runbook", "--zone", "incoming",
		"--why", "closes Judge gap"}
	if strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %v\nwant  %v", argv, want)
	}
}

func TestShellKBWriter_NoWhyOmitsFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX")
	}
	dir := t.TempDir()
	argLog := filepath.Join(dir, "argv")
	stub := filepath.Join(dir, "kb")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+argLog+"\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (&ShellKBWriter{Bin: stub}).AddEntry(context.Background(),
		"vault", "gotcha", "t", "src", ""); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if b, _ := os.ReadFile(argLog); strings.Contains(string(b), "--why") {
		t.Fatalf("--why must be omitted when why is empty:\n%s", b)
	}
}

func TestShellKBWriter_NonZeroExitIsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX")
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "kb")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho 'boom: no such area' 1>&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	id, err := (&ShellKBWriter{Bin: stub}).AddEntry(context.Background(),
		"nope", "fact", "t", "s", "")
	if err == nil {
		t.Fatal("want error on non-zero exit")
	}
	if id != "" {
		t.Fatalf("id must be empty on failure, got %q", id)
	}
	if !strings.Contains(err.Error(), "boom: no such area") {
		t.Fatalf("error should carry stderr: %v", err)
	}
}
