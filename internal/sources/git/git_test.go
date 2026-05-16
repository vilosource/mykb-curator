package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/sources"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

var _ sources.Resolver = (*Resolver)(nil)

// tempRepo builds a small git repo and returns its dir.
func tempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "# hashicorp-vault\n\n5-node Raft cluster, auto-unseal via Azure KV.\n")
	write("docker-compose.yml", "services:\n  vault:\n    image: hashicorp/vault:1.15\n")
	write("infra/main.tf", "resource \"azurerm_key_vault\" \"unseal\" {}\n")
	write("vendor/dep/README", "vendored, should rank below the root README\n")

	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"add", "-A"},
		{"commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func src(scheme, spec string) docspec.Source {
	return docspec.Source{Scheme: scheme, Spec: spec, Raw: scheme + ":" + spec}
}

func TestResolve_RepoDigest(t *testing.T) {
	dir := tempRepo(t)
	r := New("", map[string]string{"infra/hashicorp-vault": dir})

	res, ok, err := r.Resolve(context.Background(), src("git", "infra/hashicorp-vault"))
	if err != nil || !ok {
		t.Fatalf("Resolve: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(res.Digest, "5-node Raft cluster") {
		t.Errorf("README content not in digest:\n%s", res.Digest)
	}
	if !strings.Contains(res.Digest, "hashicorp/vault:1.15") {
		t.Errorf("docker-compose content not in digest:\n%s", res.Digest)
	}
	if !strings.Contains(res.Digest, "Tracked paths:") || !strings.Contains(res.Digest, "infra/main.tf") {
		t.Errorf("tracked-path listing missing:\n%s", res.Digest)
	}
	// root README must be curated before the vendored one.
	rootIdx := strings.Index(res.Digest, "#### README.md")
	vendIdx := strings.Index(res.Digest, "#### vendor/dep/README")
	if rootIdx == -1 || (vendIdx != -1 && vendIdx < rootIdx) {
		t.Errorf("root README must rank above vendored:\n%s", res.Digest)
	}
	if len(res.Refs) != 1 || !strings.HasPrefix(res.Refs[0], "git:infra/hashicorp-vault@") {
		t.Errorf("refs wrong: %v", res.Refs)
	}
	if len(res.Rows) == 0 || res.Rows[0][0] != "repo" {
		t.Errorf("first row should summarise the repo: %+v", res.Rows)
	}
}

func TestResolve_SingleFileMode(t *testing.T) {
	dir := tempRepo(t)
	r := New(filepath.Dir(dir), nil) // root = parent; repo = base name

	res, ok, err := r.Resolve(context.Background(),
		src("git", filepath.Base(dir)+" file=infra/main.tf"))
	if err != nil || !ok {
		t.Fatalf("Resolve: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(res.Digest, "azurerm_key_vault") {
		t.Errorf("file content missing:\n%s", res.Digest)
	}
	if len(res.Rows) != 1 || !strings.Contains(res.Rows[0][1], ":infra/main.tf") {
		t.Errorf("single-file row wrong: %+v", res.Rows)
	}
}

func TestResolve_PathTraversalRejected(t *testing.T) {
	dir := tempRepo(t)
	r := New("", map[string]string{"r": dir})
	if _, _, err := r.Resolve(context.Background(), src("git", "r file=../etc/passwd")); err == nil {
		t.Fatal("escaping file path must be rejected")
	}
}

func TestResolve_UnlocatableRepo_PendingNotError(t *testing.T) {
	r := New("", nil) // no root, no map → cannot locate
	res, ok, err := r.Resolve(context.Background(), src("git", "some/repo"))
	if err != nil {
		t.Fatalf("unlocatable repo must be pending (ok=false), not an error: %v", err)
	}
	if ok || res.Digest != "" {
		t.Errorf("expected empty pending result, got ok=%v res=%+v", ok, res)
	}
}

func TestResolve_NonGitDirIsHardError(t *testing.T) {
	plain := t.TempDir()
	r := New("", map[string]string{"r": plain})
	if _, _, err := r.Resolve(context.Background(), src("git", "r")); err == nil {
		t.Fatal("a located-but-non-git dir must be a hard error, not silent")
	}
}

func TestResolve_WrongSchemeIgnored(t *testing.T) {
	r := New("", nil)
	if _, ok, err := r.Resolve(context.Background(), src("ssh", "host")); ok || err != nil {
		t.Errorf("non-git scheme must no-op: ok=%v err=%v", ok, err)
	}
}

func TestParseSpec(t *testing.T) {
	repo, ref, file := parseSpec("ns/repo ref=v1.2 file=infra/main.tf")
	if repo != "ns/repo" || ref != "v1.2" || file != "infra/main.tf" {
		t.Errorf("parseSpec = %q %q %q", repo, ref, file)
	}
	if r, ref, f := parseSpec("just/repo"); r != "just/repo" || ref != "HEAD" || f != "" {
		t.Errorf("defaults wrong: %q %q %q", r, ref, f)
	}
}
