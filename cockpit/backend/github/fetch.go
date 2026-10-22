package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// rawGitHub is the locator Locate makes: owner, repository, commit, path.
var rawGitHub = regexp.MustCompile(`^https://raw\.githubusercontent\.com/([^/]+)/([^/]+)/([0-9a-f]{40})/(.+)$`)

// Handles reports whether uri is a commit-pinned GitHub locator.
func (Backend) Handles(uri string) bool { return rawGitHub.MatchString(uri) }

var client = &http.Client{Timeout: 30 * time.Second}

// Fetch reads the locator over HTTP, and when that fails (a private repository, a push that has not
// happened yet from this machine's view) from a bare clone at the pinned commit. The clone is kept in
// the user cache and fetched into when the commit is missing: it is derived, so deleting it costs
// only the next clone.
func (Backend) Fetch(ctx context.Context, uri string) ([]byte, error) {
	m := rawGitHub.FindStringSubmatch(uri)
	if m == nil {
		return nil, fmt.Errorf("not a GitHub locator: %s", uri)
	}
	b, herr := fetchHTTP(ctx, uri)
	if herr == nil {
		return b, nil
	}
	owner, name, sha, path := m[1], m[2], m[3], m[4]
	b, gerr := fetchGit(ctx, owner, name, sha, path)
	if gerr != nil {
		return nil, fmt.Errorf("%v; git %s/%s@%s: %v", herr, owner, name, sha[:12], gerr)
	}
	return b, nil
}

func fetchHTTP(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<30))
}

var cloneMu sync.Mutex

func fetchGit(ctx context.Context, owner, name, sha, path string) ([]byte, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(cache, "kton-cockpit", "fetch", "github.com", owner, name+".git")
	repo := "https://github.com/" + owner + "/" + name + ".git"
	cloneMu.Lock()
	defer cloneMu.Unlock()
	if _, err := os.Stat(dir); err != nil {
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return nil, err
		}
		if out, err := exec.CommandContext(ctx, "git", "clone", "--quiet", "--bare", repo, dir).CombinedOutput(); err != nil {
			os.RemoveAll(dir)
			return nil, fmt.Errorf("git clone: %s", strings.TrimSpace(string(out)))
		}
	} else if exec.CommandContext(ctx, "git", "-C", dir, "cat-file", "-e", sha+"^{commit}").Run() != nil {
		if out, err := exec.CommandContext(ctx, "git", "-C", dir, "fetch", "--quiet", "origin").CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git fetch: %s", strings.TrimSpace(string(out)))
		}
	}
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "show", sha+":"+path).Output()
	if err != nil {
		return nil, fmt.Errorf("git show %s:%s: %w", sha[:12], path, err)
	}
	return out, nil
}
