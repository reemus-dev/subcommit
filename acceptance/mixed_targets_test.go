package acceptance

import (
	"bytes"
	"strings"
	"testing"
)

func TestMixedTargetSelection(t *testing.T) {
	t.Run("commits a complete file and selected ranges atomically", func(t *testing.T) {
		repo := newRepository(t)
		base := []byte("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")
		repo.write("complete.txt", []byte("complete base\n"))
		repo.write("ranged.txt", base)
		repo.git("add", "complete.txt", "ranged.txt")
		repo.git("commit", "-q", "-m", "base")
		repo.write("complete.txt", []byte("complete worktree\n"))
		rangedWorktree := []byte("1\nTWO\n3\n4\n5\n6\n7\n8\n9\nTEN\n")
		repo.write("ranged.txt", rangedWorktree)

		requireSuccess(t, repo.subcommit(
			nil, nil, "complete.txt", "ranged.txt:2", "-m", "mixed selection", "--yes",
		))
		if got := repo.headFile("complete.txt"); !bytes.Equal(got, []byte("complete worktree\n")) {
			t.Fatalf("complete target content = %q", got)
		}
		if got := repo.headFile("ranged.txt"); !bytes.Equal(got, []byte("1\nTWO\n3\n4\n5\n6\n7\n8\n9\n10\n")) {
			t.Fatalf("range target content = %q", got)
		}
		if got := repo.read("ranged.txt"); !bytes.Equal(got, rangedWorktree) {
			t.Fatalf("range target worktree changed = %q", got)
		}
		if changed := repo.git("diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD"); changed != "complete.txt\nranged.txt" {
			t.Fatalf("mixed commit changed paths = %q", changed)
		}
		if status := repo.status(); !strings.Contains(status, " M ranged.txt") {
			t.Fatalf("unselected range is not preserved:\n%s", status)
		}
	})

	t.Run("merges repeated ranged arguments for one path", func(t *testing.T) {
		repo := newRepository(t)
		seedLines(repo, []byte("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n"))
		repo.write("f.txt", []byte("1\nTWO\n3\n4\n5\n6\n7\n8\n9\nTEN\n"))

		requireSuccess(t, repo.subcommit(
			nil, nil, "f.txt:2", "f.txt:10", "-m", "two ranges", "--yes",
		))
		if got := repo.headFile("f.txt"); !bytes.Equal(got, repo.read("f.txt")) {
			t.Fatalf("repeated ranges did not select both changes: %q", got)
		}
	})

	t.Run("refuses complete and ranged forms of one normalized path", func(t *testing.T) {
		repo := newRepository(t)
		seedLines(repo, []byte("one\ntwo\nthree\n"))
		repo.write("f.txt", []byte("one\nTWO\nthree\n"))
		before := repo.fingerprint()

		result := repo.subcommit(nil, nil, "f.txt", "./f.txt:2", "-m", "conflict", "--yes")
		requireRefusal(t, result)
		if !strings.Contains(result.stderr, "cannot select both a complete file and line ranges: f.txt") ||
			!strings.Contains(result.stderr, "remove either the complete path or its :ranges target") {
			t.Fatalf("conflicting-target diagnostic = %q", result.stderr)
		}
		if after := repo.fingerprint(); after != before {
			t.Fatal("conflicting target refusal changed repository state")
		}
	})
}
