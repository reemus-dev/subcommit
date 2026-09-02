package acceptance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

const processTimeout = 20 * time.Second

var (
	subcommitBinary string
	hookHelper      string
	gitProxyDir     string
	realGit         string
)

func TestMain(m *testing.M) {
	var err error
	subcommitBinary, err = selectedBinary()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	helperDir, err := os.MkdirTemp("", "subcommit-hook-helper-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	hookHelper = filepath.Join(helperDir, "hook-helper")
	gitProxyDir = filepath.Join(helperDir, "proxy")
	gitProxy := filepath.Join(gitProxyDir, "git")
	if runtime.GOOS == "windows" {
		hookHelper += ".exe"
		gitProxy += ".exe"
	}
	if err := os.Mkdir(gitProxyDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(helperDir)
		os.Exit(2)
	}
	for _, build := range []struct {
		name, output, source string
	}{
		{name: "hook helper", output: hookHelper, source: "./testhelper"},
		{name: "Git proxy", output: gitProxy, source: "./gitproxy"},
	} {
		command := exec.Command("go", "build", "-o", build.output, build.source)
		if output, buildErr := command.CombinedOutput(); buildErr != nil {
			fmt.Fprintf(os.Stderr, "build %s: %v\n%s", build.name, buildErr, output)
			_ = os.RemoveAll(helperDir)
			os.Exit(2)
		}
	}
	realGit, err = exec.LookPath("git")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(helperDir)
		os.Exit(2)
	}

	code := m.Run()
	if err := os.RemoveAll(helperDir); err != nil && code == 0 {
		fmt.Fprintln(os.Stderr, err)
		code = 2
	}
	os.Exit(code)
}

func selectedBinary() (string, error) {
	path := os.Getenv("SUBCOMMIT_TEST_BIN")
	if path == "" {
		path = filepath.Join("..", "bin", "subcommit")
		if runtime.GOOS == "windows" {
			path += ".exe"
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("acceptance binary %s: %w", absolute, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("acceptance binary %s is a directory", absolute)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("acceptance binary %s is not executable", absolute)
	}
	return absolute, nil
}

type repository struct {
	t   *testing.T
	dir string
}

func newRepository(t *testing.T) *repository {
	t.Helper()
	repo := &repository{t: t, dir: t.TempDir()}
	repo.git("init", "-q", "-b", "main")
	repo.git("config", "user.email", "acceptance@test.local")
	repo.git("config", "user.name", "Acceptance Test")
	repo.git("config", "commit.gpgsign", "false")
	repo.git("config", "core.autocrlf", "false")
	return repo
}

func (r *repository) seedBasic() {
	r.t.Helper()
	r.write("a.txt", []byte("a1\n"))
	r.write("b.txt", []byte("b1\n"))
	r.git("add", "a.txt", "b.txt")
	r.git("commit", "-q", "-m", "init")
}

func (r *repository) write(path string, content []byte) {
	r.t.Helper()
	absolute := filepath.Join(r.dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(absolute, content, 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repository) remove(path string) {
	r.t.Helper()
	if err := os.Remove(filepath.Join(r.dir, filepath.FromSlash(path))); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repository) read(path string) []byte {
	r.t.Helper()
	content, err := os.ReadFile(filepath.Join(r.dir, filepath.FromSlash(path)))
	if err != nil {
		r.t.Fatal(err)
	}
	return content
}

func (r *repository) git(args ...string) string {
	r.t.Helper()
	result := runCommand(r.t, r.dir, nil, nil, "git", args...)
	if result.exitCode != 0 {
		r.t.Fatalf("git %s failed (%d):\n%s", strings.Join(args, " "), result.exitCode, result.output())
	}
	return strings.TrimRight(result.stdout, "\r\n")
}

func (r *repository) gitResult(env map[string]string, args ...string) commandResult {
	r.t.Helper()
	return runCommand(r.t, r.dir, nil, env, "git", args...)
}

func (r *repository) subcommit(stdin []byte, env map[string]string, args ...string) commandResult {
	r.t.Helper()
	return runCommand(r.t, r.dir, stdin, env, subcommitBinary, args...)
}

func (r *repository) head() string {
	r.t.Helper()
	return r.git("rev-parse", "HEAD")
}

func (r *repository) headFile(path string) []byte {
	r.t.Helper()
	result := r.gitResult(nil, "show", "HEAD:"+path)
	if result.exitCode != 0 {
		r.t.Fatalf("read HEAD:%s: %s", path, result.output())
	}
	return []byte(result.stdout)
}

func (r *repository) index() string {
	r.t.Helper()
	return r.git("-c", "core.quotePath=false", "ls-files", "--stage")
}

func (r *repository) indexEntry(path string) string {
	r.t.Helper()
	return r.git("ls-files", "--stage", "--", path)
}

func (r *repository) status() string {
	r.t.Helper()
	return r.git("-c", "core.quotePath=false", "status", "--porcelain=v1")
}

func (r *repository) installHook(name string) {
	r.t.Helper()
	content, err := os.ReadFile(hookHelper)
	if err != nil {
		r.t.Fatal(err)
	}
	path := filepath.Join(r.dir, ".git", "hooks", name)
	if err := os.WriteFile(path, content, 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repository) fingerprint() string {
	r.t.Helper()
	parts := []string{
		"STATUS\x00" + r.git("-c", "core.quotePath=false", "status", "--porcelain=v1", "-z"),
		"INDEX\x00" + r.git("-c", "core.quotePath=false", "ls-files", "--stage", "-z"),
		"TREE\x00" + r.git("-c", "core.quotePath=false", "ls-tree", "-r", "HEAD"),
	}

	var files []string
	err := filepath.WalkDir(r.dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == filepath.Join(r.dir, ".git") {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(r.dir, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		executable := info.Mode().Perm()&0o111 != 0
		digest := sha256.Sum256(content)
		files = append(files, fmt.Sprintf(
			"%s\x00%t\x00%x", filepath.ToSlash(relative), executable, digest,
		))
		return nil
	})
	if err != nil {
		r.t.Fatal(err)
	}
	sort.Strings(files)
	parts = append(parts, "WORKTREE\x00"+strings.Join(files, "\x00"))
	return strings.Join(parts, "\n")
}

type commandResult struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
}

func (r commandResult) output() string {
	return r.stdout + r.stderr
}

func runCommand(
	t *testing.T,
	dir string,
	stdin []byte,
	overrides map[string]string,
	executable string,
	args ...string,
) commandResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), processTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = dir
	command.Env = commandEnvironment(overrides)
	command.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	result := commandResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
	if err == nil {
		return result
	}
	if ctx.Err() != nil {
		t.Fatalf("%s timed out: %v", executable, ctx.Err())
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("start %s: %v", executable, err)
	}
	result.exitCode = exitError.ExitCode()
	return result
}

func commandEnvironment(overrides map[string]string) []string {
	values := make(map[string]string, len(os.Environ())+len(overrides)+2)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	values["GIT_AUTHOR_DATE"] = "2000-01-01T00:00:00Z"
	values["GIT_COMMITTER_DATE"] = "2000-01-01T00:00:00Z"
	for key, value := range overrides {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return environment
}

func requireSuccess(t *testing.T, result commandResult) {
	t.Helper()
	if result.exitCode != 0 {
		t.Fatalf("command failed (%d):\n%s", result.exitCode, result.output())
	}
	if !strings.HasPrefix(result.stdout, "committed: ") ||
		strings.Count(strings.TrimSpace(result.stdout), "\n") != 0 {
		t.Fatalf("invalid stable success record: %q", result.stdout)
	}
	if strings.Contains(result.stderr, "committed: ") {
		t.Fatalf("success record leaked to stderr: %q", result.stderr)
	}
}

func requireRefusal(t *testing.T, result commandResult) {
	t.Helper()
	if result.exitCode == 0 {
		t.Fatalf("command unexpectedly succeeded:\n%s", result.output())
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}
