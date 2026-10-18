package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func filepathJoin(t *testing.T, dir, name string) string {
	t.Helper()
	return filepath.Join(dir, name)
}

func initGitRepo(t *testing.T, root string) string {
	t.Helper()
	repo := filepath.Join(root, "repo")
	if out, e := runGit(t, repo, "init", "-q"); e != nil {
		t.Fatalf("init: %s %v", out, e)
	}
	if out, e := runGit(t, repo, "remote", "add", "origin", "https://github.com/owner/repo.git"); e != nil {
		t.Fatalf("remote add: %s %v", out, e)
	}
	return repo
}

func gitCfg(t *testing.T, repo string, args ...string) (string, error) {
	t.Helper()
	full := append([]string{"config"}, args...)
	return runGit(t, repo, full...)
}

func runGit(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	if e := os.MkdirAll(dir, 0o755); e != nil {
		return "", e
	}
	cmd := exec.Command("git", Args(dir, args...)...)
	cmd.Env = Env()
	out, e := cmd.CombinedOutput()
	return string(out), e
}

func runGitEnv(t *testing.T, dir string, env []string, args ...string) (string, error) {
	t.Helper()
	if e := os.MkdirAll(dir, 0o755); e != nil {
		return "", e
	}
	cmd := exec.Command("git", Args(dir, args...)...)
	cmd.Env = append(Env(), env...)
	out, e := cmd.CombinedOutput()
	return string(out), e
}

func envAuthor() []string {
	return []string{
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid",
	}
}

func gitopsHead(t *testing.T, dir string) (string, error) {
	t.Helper()
	out, e := runGit(t, dir, "rev-parse", "HEAD")
	return strings.TrimSpace(out), e
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); e != nil {
		t.Fatal(e)
	}
}

const testSHA5 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
