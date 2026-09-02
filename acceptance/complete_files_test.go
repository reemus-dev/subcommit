package acceptance

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompleteFileSelectionAndPreservation(t *testing.T) {
	t.Parallel()
	t.Run("preserves unrelated staged and unstaged state", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("a-worktree\n"))
		repo.write("b.txt", []byte("b-staged\n"))
		repo.git("add", "b.txt")
		stagedB := repo.indexEntry("b.txt")
		repo.write("b.txt", []byte("b-unstaged\n"))
		repo.write("untracked.txt", []byte("untouched\n"))
		beforeB := append([]byte(nil), repo.read("b.txt")...)
		beforeUntracked := append([]byte(nil), repo.read("untracked.txt")...)

		result := repo.subcommit(nil, nil, "a.txt", "-m", "commit a", "--yes")
		requireSuccess(t, result)
		if !strings.Contains(result.stderr, "Other changes (2 paths, not committed)") ||
			!strings.Contains(result.stderr, "MM b.txt") ||
			!strings.Contains(result.output(), "?? untracked.txt") {
			t.Fatalf("preserved-state preview missing:\n%s", result.output())
		}

		if got := repo.headFile("a.txt"); !bytes.Equal(got, []byte("a-worktree\n")) {
			t.Fatalf("committed a.txt = %q", got)
		}
		if got := repo.indexEntry("b.txt"); got != stagedB {
			t.Fatalf("b.txt index changed:\nwant %s\n got %s", stagedB, got)
		}
		if got := repo.read("b.txt"); !bytes.Equal(got, beforeB) {
			t.Fatalf("b.txt worktree changed: %q", got)
		}
		if got := repo.read("untracked.txt"); !bytes.Equal(got, beforeUntracked) {
			t.Fatalf("untracked file changed: %q", got)
		}
		status := repo.status()
		for _, line := range []string{"MM b.txt", "?? untracked.txt"} {
			if !strings.Contains(status, line) {
				t.Fatalf("status missing %q:\n%s", line, status)
			}
		}
	})

	t.Run("refuses staged changes absent from a selected worktree", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("selected staging\n"))
		repo.git("add", "a.txt")
		repo.write("a.txt", []byte("a1\n"))
		repo.write("b.txt", []byte("selected worktree\n"))
		before := repo.fingerprint()

		result := repo.subcommit(nil, nil, "a.txt", "b.txt", "-m", "commit selected", "--yes")
		requireRefusal(t, result)
		if !strings.Contains(result.stderr, "selected path has staged changes absent from its worktree: a.txt") {
			t.Fatalf("staged-only refusal absent:\n%s", result.stderr)
		}
		if got := repo.fingerprint(); got != before {
			t.Fatal("staged-only refusal changed repository state")
		}
	})

	t.Run("adds untracked file", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("new.bin", []byte{0, 1, 2, 3, 255})

		requireSuccess(t, repo.subcommit(nil, nil, "new.bin", "-m", "add binary", "--yes"))
		if got := repo.headFile("new.bin"); !bytes.Equal(got, []byte{0, 1, 2, 3, 255}) {
			t.Fatalf("committed binary = %v", got)
		}
		if got := repo.status(); got != "" {
			t.Fatalf("new target should be clean, status = %q", got)
		}
	})

	t.Run("commits executable mode", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows worktrees do not expose Unix executable mode")
		}
		repo := newRepository(t)
		repo.seedBasic()
		repo.git("config", "core.fileMode", "true")
		if err := os.Chmod(filepath.Join(repo.dir, "a.txt"), 0o755); err != nil {
			t.Fatal(err)
		}

		requireSuccess(t, repo.subcommit(nil, nil, "a.txt", "-m", "make executable", "--yes"))
		entry := repo.git("ls-tree", "HEAD", "--", "a.txt")
		if !strings.HasPrefix(entry, "100755 ") {
			t.Fatalf("tree entry does not preserve executable mode: %s", entry)
		}
	})
}

func TestCompleteFileSkipWorktreeSelection(t *testing.T) {
	t.Parallel()
	t.Run("absent skipped target remains unchanged", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.git("update-index", "--skip-worktree", "a.txt")
		repo.remove("a.txt")
		beforeHead := repo.head()
		beforeIndex := repo.git("ls-files", "--stage", "-v", "--", "a.txt")
		if !strings.HasPrefix(beforeIndex, "S ") {
			t.Fatalf("target is not skip-worktree: %s", beforeIndex)
		}

		result := repo.subcommit(nil, nil, "a.txt", "-m", "skip", "--yes")
		requireRefusal(t, result)
		if !strings.Contains(result.stderr, "error: nothing to commit") ||
			!strings.Contains(result.stderr, "absent from this worktree") {
			t.Fatalf("no-change diagnostic missing: %s", result.stderr)
		}
		if repo.head() != beforeHead {
			t.Fatal("absent skipped request moved HEAD")
		}
		if got := repo.git("ls-files", "--stage", "-v", "--", "a.txt"); got != beforeIndex {
			t.Fatalf("skip-worktree index entry changed:\nwant %s\n got %s", beforeIndex, got)
		}
		if _, err := os.Stat(filepath.Join(repo.dir, "a.txt")); !os.IsNotExist(err) {
			t.Fatalf("skipped target materialized in worktree: %v", err)
		}
	})

	t.Run("present skipped target commits worktree content", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("old staged content\n"))
		repo.git("add", "a.txt")
		repo.git("update-index", "--skip-worktree", "a.txt")
		repo.write("a.txt", []byte("selected worktree content\n"))
		beforeIndex := repo.git("ls-files", "--stage", "-v", "--", "a.txt")
		if !strings.HasPrefix(beforeIndex, "S ") {
			t.Fatalf("target is not skip-worktree: %s", beforeIndex)
		}

		requireSuccess(t, repo.subcommit(nil, nil, "a.txt", "-m", "select present", "--yes"))
		if got := repo.headFile("a.txt"); !bytes.Equal(got, []byte("selected worktree content\n")) {
			t.Fatalf("selected HEAD content = %q", got)
		}
		if got := repo.read("a.txt"); !bytes.Equal(got, []byte("selected worktree content\n")) {
			t.Fatalf("selected worktree content changed to %q", got)
		}
		if result := repo.gitResult(
			nil, "diff", "--cached", "--quiet", "HEAD", "--", "a.txt",
		); result.exitCode != 0 {
			t.Fatalf("selected index does not equal HEAD: %s", result.output())
		}
	})

	t.Run("mixed absent skipped and ordinary target refuses", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.git("update-index", "--skip-worktree", "a.txt")
		repo.remove("a.txt")
		repo.write("b.txt", []byte("ordinary change\n"))
		before := repo.fingerprint()

		result := repo.subcommit(nil, nil, "a.txt", "b.txt", "-m", "mixed", "--yes")
		requireRefusal(t, result)
		if !strings.Contains(result.stderr, "not every selected target contains a committable change") ||
			!strings.Contains(result.stderr, "a.txt  absent from this worktree") {
			t.Fatalf("mixed skip refusal absent:\n%s", result.stderr)
		}
		if got := repo.fingerprint(); got != before {
			t.Fatal("mixed skip refusal changed repository state")
		}
	})
}
