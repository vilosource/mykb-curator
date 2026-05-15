//go:build scenario

package scenario_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// mediawikiFixture is a freshly-installed MediaWiki container with a
// known admin account. Shared by every scenario in this package.
//
// What's done:
//   - Start mediawiki:1.41 with the bind-mounted data dir
//   - Wait for Apache to serve /
//   - Exec install.php with SQLite backend + admin/adminpw credentials
//   - Wait for /api.php to return 200
//
// The Admin account is used as both the operator and the "bot" for
// curator writes. A real deployment would create a separate bot via
// Special:BotPasswords, but that flow is web-form-only and out of
// scope for the v0.5 scenario.
type mediawikiFixture struct {
	container testcontainers.Container
	URL       string
	AdminUser string
	AdminPass string
	WikiName  string
}

const (
	mediawikiImage    = "mediawiki:1.41"
	scenarioAdmin     = "Admin"
	scenarioAdminPass = "adminpassword-9999" // ≥10 chars, MW minimum
	scenarioWikiName  = "ScenarioWiki"
)

func startMediaWiki(t *testing.T) *mediawikiFixture {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        mediawikiImage,
		ExposedPorts: []string{"80/tcp"},
		WaitingFor:   wait.ForHTTP("/").WithPort("80/tcp").WithStartupTimeout(2 * time.Minute),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start mediawiki container: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Terminate(ctx)
	})

	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	port, err := c.MappedPort(ctx, "80/tcp")
	if err != nil {
		t.Fatalf("port: %v", err)
	}
	url := fmt.Sprintf("http://%s:%s", host, port.Port())

	// Ensure /var/www/data exists + is writable by www-data — the
	// mediawiki image doesn't pre-create it.
	rc, out, err := c.Exec(ctx, []string{"sh", "-c", "mkdir -p /var/www/data && chown -R www-data:www-data /var/www/data"})
	if err != nil {
		t.Fatalf("mkdir data exec: %v", err)
	}
	if rc != 0 {
		body, _ := readStreamingOutput(out)
		t.Fatalf("mkdir data exit %d:\n%s", rc, body)
	}

	// Install MediaWiki inside the container. SQLite avoids needing
	// a companion mariadb container. Server URL must match the
	// externally-visible host:port so the API generates correctly-
	// rooted links.
	installCmd := []string{
		"php", "/var/www/html/maintenance/install.php",
		"--dbtype=sqlite",
		"--dbpath=/var/www/data",
		"--pass=" + scenarioAdminPass,
		"--server=" + url,
		"--scriptpath=",
		scenarioWikiName, scenarioAdmin,
	}
	rc, out, err = c.Exec(ctx, installCmd)
	if err != nil {
		t.Fatalf("install.php exec: %v", err)
	}
	installLog, _ := readStreamingOutput(out)
	if rc != 0 {
		t.Fatalf("install.php exit %d:\n%s", rc, installLog)
	}
	t.Logf("install.php stdout:\n%s", installLog)

	// install.php ran as root (Exec default user); SQLite DB +
	// LocalSettings.php are root-owned and unreadable by www-data,
	// which serves the wiki. Fix ownership before Apache reads them.
	rc, out, _ = c.Exec(ctx, []string{"sh", "-c", `
		chown -R www-data:www-data /var/www/data /var/www/html/LocalSettings.php
		chmod 644 /var/www/html/LocalSettings.php
		ls -la /var/www/html/LocalSettings.php /var/www/data/
	`})
	body, _ := readStreamingOutput(out)
	t.Logf("perm fix-up (rc=%d):\n%s", rc, body)

	// Promote Admin into the "bot" group. The curator's MediaWikiTarget
	// asserts bot rights on every edit (production-grade safety: don't
	// pose as a bot you're not). createAndPromote --force promotes an
	// existing user without re-creating it.
	rc, out, _ = c.Exec(ctx, []string{
		"php", "/var/www/html/maintenance/createAndPromote.php",
		"--bot", "--force", scenarioAdmin, scenarioAdminPass,
	})
	body, _ = readStreamingOutput(out)
	if rc != 0 {
		t.Fatalf("promote to bot exit %d:\n%s", rc, body)
	}
	t.Logf("promote to bot (rc=%d):\n%s", rc, body)

	// Wait for /api.php to return real JSON (not the installer page).
	if err := waitForAPI(url, 30*time.Second); err != nil {
		t.Fatalf("/api.php never returned JSON: %v\nlast Localsettings status:\n%s", err, body)
	}

	return &mediawikiFixture{
		container: c,
		URL:       url,
		AdminUser: scenarioAdmin,
		AdminPass: scenarioAdminPass,
		WikiName:  scenarioWikiName,
	}
}

func waitForHTTP(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timeout after %s", timeout)
}

// waitForAPI polls /api.php?action=query&meta=siteinfo&format=json
// until the response is real JSON (post-install) rather than the
// installer's HTML response. We discriminate by the Content-Type
// header: installed wiki returns application/json; installer page
// returns text/html.
func waitForAPI(base string, timeout time.Duration) error {
	url := base + "/api.php?action=query&meta=siteinfo&format=json"
	deadline := time.Now().Add(timeout)
	var lastStatus, lastCT string
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			ct := resp.Header.Get("Content-Type")
			lastStatus = resp.Status
			lastCT = ct
			_ = resp.Body.Close()
			if resp.StatusCode == 200 && strings.Contains(ct, "json") {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timeout after %s; last status=%q content-type=%q", timeout, lastStatus, lastCT)
}

// fetchPageContent queries action=parse&prop=wikitext to get the
// page's wikitext directly. Used by scenarios as a workaround for
// the MediaWikiTarget.GetPage roundtrip fragility on fresh-install
// MediaWiki.
func fetchPageContent(ctx context.Context, wikiURL, title string) (string, error) {
	u := wikiURL + "/api.php?action=parse&page=" + url.QueryEscape(title) +
		"&prop=wikitext&format=json"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var raw struct {
		Parse struct {
			Wikitext struct {
				All string `json:"*"`
			} `json:"wikitext"`
		} `json:"parse"`
		Error *struct {
			Code string `json:"code"`
			Info string `json:"info"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", fmt.Errorf("decode parse response: %w; body=%s", err, body)
	}
	if raw.Error != nil {
		return "", fmt.Errorf("parse api error %s: %s", raw.Error.Code, raw.Error.Info)
	}
	return raw.Parse.Wikitext.All, nil
}

// newCookieJar returns a fresh in-memory cookie jar for HTTP
// sessions. Used by scenarios that drive MediaWiki's auth+edit
// dance directly (bypassing the curator's adapter so we can
// simulate non-bot edits).
func newCookieJar() (http.CookieJar, error) {
	return cookiejar.New(nil)
}

// postForJSONField does a form POST (or GET if form is nil) and
// reads a single field from the response JSON via dotted path.
func postForJSONField(ctx context.Context, c *http.Client, urlStr string, form url.Values, dotPath string) (string, error) {
	var (
		resp *http.Response
		err  error
	)
	if form == nil {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
		resp, err = c.Do(req)
	} else {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err = c.Do(req)
	}
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", fmt.Errorf("decode json (%s): %w; body=%s", urlStr, err, body)
	}
	parts := strings.Split(dotPath, ".")
	var cur any = raw
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("path %s: not a map at %s; body=%s", dotPath, p, body)
		}
		cur = m[p]
	}
	s, _ := cur.(string)
	if s == "" {
		return "", fmt.Errorf("path %s: empty/non-string; body=%s", dotPath, body)
	}
	return s, nil
}

// readStreamingOutput drains a testcontainers exec output reader.
// Best-effort — used only on failure paths.
func readStreamingOutput(r interface {
	Read([]byte) (int, error)
}) (string, error) {
	if r == nil {
		return "", nil
	}
	var sb strings.Builder
	buf := make([]byte, 8192)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return sb.String(), nil
}
