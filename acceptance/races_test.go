package acceptance

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestGuardedHEADRejectsExternalMove(t *testing.T) {
	repo := newRepository(t)
	repo.seedBasic()
	repo.write("a.txt", []byte("candidate\n"))
	repo.installHook("pre-commit")
	originalHead := repo.head()
	originalIndex := repo.index()
	originalWorktree := append([]byte(nil), repo.read("a.txt")...)
	ready := filepath.Join(repo.dir, ".git", "guard-ready")
	release := filepath.Join(repo.dir, ".git", "guard-release")
	env := map[string]string{
		"SUBCOMMIT_HOOK_PRE_COMMIT": "barrier",
		"SUBCOMMIT_HOOK_READY":      ready,
		"SUBCOMMIT_HOOK_RELEASE":    release,
	}

	process := startSubcommit(t, repo, env, "a.txt", "-m", "candidate", "--yes")
	waitForFile(t, ready)

	tree := repo.git("rev-parse", originalHead+"^{tree}")
	externalHead := repo.git("commit-tree", tree, "-p", originalHead, "-m", "external winner")
	repo.git("update-ref", "HEAD", externalHead, originalHead)
	repo.write(".git/guard-release", nil)

	result := process.wait(t)
	requireRefusal(t, result)
	if !strings.Contains(result.stderr, "repository changed before the commit could be published") {
		t.Fatalf("missing guarded-HEAD refusal evidence:\n%s", result.stderr)
	}
	if got := repo.head(); got != externalHead {
		t.Fatalf("external HEAD was overwritten: want %s, got %s", externalHead, got)
	}
	if got := repo.index(); got != originalIndex {
		t.Fatalf("real index changed:\nwant %s\n got %s", originalIndex, got)
	}
	if got := repo.read("a.txt"); !bytes.Equal(got, originalWorktree) {
		t.Fatalf("worktree changed: %q", got)
	}
	if parent := repo.git("rev-parse", "HEAD^"); parent != originalHead {
		t.Fatalf("external commit parent changed: want %s, got %s", originalHead, parent)
	}
}

func TestSimultaneousCommitsHaveOneWinnerAndRetry(t *testing.T) {
	repo := newRepository(t)
	repo.seedBasic()
	repo.write("a.txt", []byte("a-candidate\n"))
	repo.write("b.txt", []byte("b-candidate\n"))
	repo.installHook("pre-commit")
	originalHead := repo.head()
	originalAIndex := repo.indexEntry("a.txt")
	originalBIndex := repo.indexEntry("b.txt")
	release := filepath.Join(repo.dir, ".git", "race-release")
	readyA := filepath.Join(repo.dir, ".git", "race-a-ready")
	readyB := filepath.Join(repo.dir, ".git", "race-b-ready")

	processA := startSubcommit(
		t, repo, barrierEnvironment(readyA, release),
		"a.txt", "-m", "candidate a", "--yes",
	)
	processB := startSubcommit(
		t, repo, barrierEnvironment(readyB, release),
		"b.txt", "-m", "candidate b", "--yes",
	)
	waitForFile(t, readyA)
	waitForFile(t, readyB)
	repo.write(".git/race-release", nil)

	resultA := processA.wait(t)
	resultB := processB.wait(t)
	successes := 0
	if resultA.exitCode == 0 {
		successes++
	}
	if resultB.exitCode == 0 {
		successes++
	}
	if successes != 1 {
		t.Fatalf(
			"expected one winner; a exit=%d, b exit=%d\na: %s\nb: %s",
			resultA.exitCode,
			resultB.exitCode,
			resultA.output(),
			resultB.output(),
		)
	}

	winner, loser := "a.txt", "b.txt"
	loserResult := resultB
	loserOriginalIndex := originalBIndex
	if resultB.exitCode == 0 {
		winner, loser = "b.txt", "a.txt"
		loserResult = resultA
		loserOriginalIndex = originalAIndex
	}
	if !strings.Contains(loserResult.stderr, "repository changed before the commit could be published") &&
		!strings.Contains(loserResult.stderr, "Git is already updating the repository") {
		t.Fatalf("loser lacks concurrency refusal evidence:\n%s", loserResult.stderr)
	}
	if !strings.Contains(loserResult.stderr, "Candidate commit") ||
		!strings.Contains(loserResult.stderr, "git cherry-pick") {
		t.Fatalf("loser lacks candidate recovery guidance:\n%s", loserResult.stderr)
	}
	if parent := repo.git("rev-parse", "HEAD^"); parent != originalHead {
		t.Fatalf("winner did not commit from shared starting HEAD: want %s, got %s", originalHead, parent)
	}
	changed := repo.git("diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD")
	if changed != winner {
		t.Fatalf("winner commit changed %q, want only %q", changed, winner)
	}
	if got := repo.indexEntry(loser); got != loserOriginalIndex {
		t.Fatalf("loser mutated its real index entry:\nwant %s\n got %s", loserOriginalIndex, got)
	}
	if result := repo.gitResult(
		nil, "diff", "--cached", "--quiet", "HEAD", "--", winner,
	); result.exitCode != 0 {
		t.Fatalf("winner index was not finalized: %s", result.output())
	}
	if !strings.Contains(repo.status(), " M "+loser) {
		t.Fatalf("loser change is not available for retry:\n%s", repo.status())
	}

	retry := repo.subcommit(nil, nil, loser, "-m", "retry loser", "--yes", "--no-verify")
	requireSuccess(t, retry)
	if got := repo.status(); got != "" {
		t.Fatalf("retry did not consume remaining target cleanly: %q", got)
	}
	if got := repo.headFile("a.txt"); !bytes.Equal(got, []byte("a-candidate\n")) {
		t.Fatalf("a.txt after retry = %q", got)
	}
	if got := repo.headFile("b.txt"); !bytes.Equal(got, []byte("b-candidate\n")) {
		t.Fatalf("b.txt after retry = %q", got)
	}
}

func TestIndexPublicationRaceAfterHEADMove(t *testing.T) {
	requireLockProtocol(t)
	repo := newRepository(t)
	repo.seedBasic()
	repo.write("a.txt", []byte("subcommit candidate\n"))
	originalIndex := repo.indexEntry("a.txt")
	ready := filepath.Join(repo.dir, ".git", "publication-ready")
	release := filepath.Join(repo.dir, ".git", "publication-release")
	env := map[string]string{
		"PATH":                        gitProxyDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"SUBCOMMIT_REAL_GIT":          realGit,
		"SUBCOMMIT_GIT_PROXY_READY":   ready,
		"SUBCOMMIT_GIT_PROXY_RELEASE": release,
	}

	process := startSubcommit(
		t, repo, env,
		"a.txt", "-m", "candidate", "--yes", "--no-verify",
	)
	waitForFile(t, ready)
	if got := repo.indexEntry("a.txt"); got != originalIndex {
		t.Fatalf("real index changed before publication:\nwant %s\n got %s", originalIndex, got)
	}

	repo.write("a.txt", []byte("competing stage\n"))
	competing := repo.gitResult(nil, "add", "--", "a.txt")
	if competing.exitCode == 0 || !strings.Contains(competing.output(), "index.lock") {
		t.Fatalf("competing git add was not excluded by the held index lock:\n%s", competing.output())
	}
	repo.write(".git/publication-release", nil)
	requireSuccess(t, process.wait(t))

	if got := repo.read("a.txt"); !bytes.Equal(got, []byte("competing stage\n")) {
		t.Fatalf("worktree competitor was overwritten: %q", got)
	}
	if result := repo.gitResult(
		nil, "diff", "--cached", "--quiet", "HEAD", "--", "a.txt",
	); result.exitCode != 0 {
		t.Fatalf("published index is not aligned with the created commit: %s", result.output())
	}
	repo.git("add", "--", "a.txt")
	if result := repo.gitResult(
		nil, "diff", "--cached", "--quiet", "HEAD", "--", "a.txt",
	); result.exitCode == 0 {
		t.Fatal("competing target could not be staged after publication")
	}
}

func TestFinalIndexLockContentionReportsOrphanCandidate(t *testing.T) {
	requireLockProtocol(t)
	repo := newRepository(t)
	repo.seedBasic()
	repo.write("a.txt", []byte("candidate\n"))
	originalHead, originalIndex := repo.head(), repo.index()
	ready := filepath.Join(repo.dir, ".git", "lock-contention-ready")
	release := filepath.Join(repo.dir, ".git", "lock-contention-release")
	env := map[string]string{
		"PATH":                        gitProxyDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"SUBCOMMIT_REAL_GIT":          realGit,
		"SUBCOMMIT_GIT_PROXY_COMMAND": "commit-tree",
		"SUBCOMMIT_GIT_PROXY_READY":   ready,
		"SUBCOMMIT_GIT_PROXY_RELEASE": release,
	}

	process := startSubcommit(
		t, repo, env,
		"a.txt", "-m", "candidate", "--yes", "--no-verify",
	)
	waitForFile(t, ready)
	repo.write(".git/index.lock", nil)
	repo.write(".git/lock-contention-release", nil)
	result := process.wait(t)
	if err := os.Remove(filepath.Join(repo.dir, ".git", "index.lock")); err != nil {
		t.Fatal(err)
	}

	requireRefusal(t, result)
	if !strings.Contains(result.stderr, "Git is already updating the repository") ||
		strings.Contains(result.stderr, "repository changed before the commit could be published") {
		t.Fatalf("lock contention was conflated with a HEAD race:\n%s", result.stderr)
	}
	if !strings.Contains(result.stderr, "Candidate commit") ||
		!strings.Contains(result.stderr, "git cherry-pick") {
		t.Fatalf("missing candidate recovery guidance:\n%s", result.stderr)
	}
	if repo.head() != originalHead || repo.index() != originalIndex {
		t.Fatal("final lock contention changed HEAD or index")
	}
}

func TestSelectedIndexRacePublishesCapturedWorktree(t *testing.T) {
	requireLockProtocol(t)
	repo := newRepository(t)
	repo.seedBasic()
	repo.write("a.txt", []byte("subcommit candidate\n"))
	repo.write("b.txt", []byte("unrelated staging\n"))
	repo.git("add", "--", "b.txt")
	unrelatedIndex := repo.indexEntry("b.txt")
	ready := filepath.Join(repo.dir, ".git", "selected-race-ready")
	release := filepath.Join(repo.dir, ".git", "selected-race-release")
	env := map[string]string{
		"PATH":                        gitProxyDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"SUBCOMMIT_REAL_GIT":          realGit,
		"SUBCOMMIT_GIT_PROXY_COMMAND": "commit-tree",
		"SUBCOMMIT_GIT_PROXY_READY":   ready,
		"SUBCOMMIT_GIT_PROXY_RELEASE": release,
	}

	process := startSubcommit(
		t, repo, env,
		"a.txt", "-m", "candidate", "--yes", "--no-verify",
	)
	waitForFile(t, ready)
	repo.write("a.txt", []byte("externally staged\n"))
	repo.git("add", "--", "a.txt")
	repo.write("a.txt", []byte("later worktree edit\n"))
	repo.write(".git/selected-race-release", nil)

	requireSuccess(t, process.wait(t))
	if got := repo.headFile("a.txt"); !bytes.Equal(got, []byte("subcommit candidate\n")) {
		t.Fatalf("committed selected content = %q", got)
	}
	if result := repo.gitResult(
		nil, "diff", "--cached", "--quiet", "HEAD", "--", "a.txt",
	); result.exitCode != 0 {
		t.Fatalf("selected index is not aligned with HEAD: %s", result.output())
	}
	if got := repo.read("a.txt"); !bytes.Equal(got, []byte("later worktree edit\n")) {
		t.Fatalf("later worktree edit changed: %q", got)
	}
	if result := repo.gitResult(nil, "diff", "--quiet", "HEAD", "--", "a.txt"); result.exitCode == 0 {
		t.Fatal("later worktree edit is not unstaged")
	}
	if got := repo.indexEntry("b.txt"); got != unrelatedIndex {
		t.Fatalf("unrelated staged entry changed:\nwant %s\n got %s", unrelatedIndex, got)
	}
}

func TestTerminationBeforeHEADMoveCleansTransaction(t *testing.T) {
	requireLockProtocol(t)
	if runtime.GOOS == "windows" {
		t.Skip("Process.Signal does not provide SIGTERM semantics on Windows")
	}
	repo := newRepository(t)
	repo.seedBasic()
	repo.write("a.txt", []byte("candidate\n"))
	repo.installHook("pre-commit")
	originalHead, originalIndex := repo.head(), repo.index()
	ready := filepath.Join(repo.dir, ".git", "signal-ready")
	release := filepath.Join(repo.dir, ".git", "signal-release")

	process := startSubcommit(
		t, repo, barrierEnvironment(ready, release),
		"a.txt", "-m", "candidate", "--yes",
	)
	waitForFile(t, ready)
	process.signal(t, syscall.SIGTERM)
	repo.write(".git/signal-release", nil)
	requireRefusal(t, process.wait(t))
	if repo.head() != originalHead || repo.index() != originalIndex {
		t.Fatal("termination before HEAD movement changed repository state")
	}
	if _, err := os.Stat(filepath.Join(repo.dir, ".git", "index.lock")); !os.IsNotExist(err) {
		t.Fatalf("index lock remains after termination: %v", err)
	}
}

func TestTerminationAfterHEADMoveCompletesPublication(t *testing.T) {
	requireLockProtocol(t)
	if runtime.GOOS == "windows" {
		t.Skip("Process.Signal does not provide SIGTERM semantics on Windows")
	}
	repo := newRepository(t)
	repo.seedBasic()
	repo.write("a.txt", []byte("candidate\n"))
	ready := filepath.Join(repo.dir, ".git", "signal-publication-ready")
	release := filepath.Join(repo.dir, ".git", "signal-publication-release")
	env := map[string]string{
		"PATH":                        gitProxyDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"SUBCOMMIT_REAL_GIT":          realGit,
		"SUBCOMMIT_GIT_PROXY_READY":   ready,
		"SUBCOMMIT_GIT_PROXY_RELEASE": release,
	}

	process := startSubcommit(
		t, repo, env,
		"a.txt", "-m", "candidate", "--yes", "--no-verify",
	)
	waitForFile(t, ready)
	process.signal(t, syscall.SIGTERM)
	repo.write(".git/signal-publication-release", nil)
	requireSuccess(t, process.wait(t))
	if result := repo.gitResult(
		nil, "diff", "--cached", "--quiet", "HEAD", "--", "a.txt",
	); result.exitCode != 0 {
		t.Fatalf("index was not safely published after termination: %s", result.output())
	}
	if _, err := os.Stat(filepath.Join(repo.dir, ".git", "index.lock")); !os.IsNotExist(err) {
		t.Fatalf("index lock remains after publication: %v", err)
	}
}

func requireLockProtocol(t *testing.T) {
	t.Helper()
	if os.Getenv("SUBCOMMIT_TEST_LOCK_PROTOCOL") != "1" {
		t.Skip("set SUBCOMMIT_TEST_LOCK_PROTOCOL=1 for Go lock-protocol tests")
	}
}

func barrierEnvironment(ready, release string) map[string]string {
	return map[string]string{
		"SUBCOMMIT_HOOK_PRE_COMMIT": "barrier",
		"SUBCOMMIT_HOOK_READY":      ready,
		"SUBCOMMIT_HOOK_RELEASE":    release,
	}
}

type runningCommand struct {
	cancel  context.CancelFunc
	process *os.Process
	done    chan commandResult
}

func startSubcommit(
	t *testing.T, repo *repository,
	env map[string]string, args ...string,
) *runningCommand {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), processTimeout)
	command := exec.CommandContext(ctx, subcommitBinary, args...)
	command.Dir = repo.dir
	command.Env = commandEnvironment(env)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		cancel()
		t.Fatalf("start subcommit: %v", err)
	}

	done := make(chan commandResult, 1)
	go func() {
		err := command.Wait()
		result := commandResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
		if err != nil {
			result.exitCode = -1
			var exitError *exec.ExitError
			if errors.As(err, &exitError) {
				result.exitCode = exitError.ExitCode()
			}
		}
		done <- result
	}()
	return &runningCommand{cancel: cancel, process: command.Process, done: done}
}

func (process *runningCommand) signal(t *testing.T, signal os.Signal) {
	t.Helper()
	if err := process.process.Signal(signal); err != nil {
		t.Fatalf("signal subcommit process: %v", err)
	}
}

func (process *runningCommand) wait(t *testing.T) commandResult {
	t.Helper()
	defer process.cancel()
	select {
	case result := <-process.done:
		if result.exitCode == -1 {
			t.Fatalf("subcommit process failed to run: %v\n%s", result.err, result.output())
		}
		return result
	case <-time.After(processTimeout + time.Second):
		t.Fatal("timed out waiting for subcommit process")
		return commandResult{}
	}
}
