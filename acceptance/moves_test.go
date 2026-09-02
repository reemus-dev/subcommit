package acceptance

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExactMoveSelection(t *testing.T) {
	t.Parallel()
	t.Run("selecting destination includes source", func(t *testing.T) {
		repo := newRepository(t)
		repo.write("old.txt", []byte("moved\n"))
		repo.write("other.txt", []byte("base\n"))
		repo.git("add", "old.txt", "other.txt")
		repo.git("commit", "-q", "-m", "base")
		repo.git("mv", "old.txt", "new.txt")
		repo.write("other.txt", []byte("staged\n"))
		repo.git("add", "other.txt")
		otherIndex := repo.indexEntry("other.txt")

		requireSuccess(t, repo.subcommit(nil, nil, "new.txt", "-m", "move", "--yes"))
		if result := repo.gitResult(nil, "cat-file", "-e", "HEAD:old.txt"); result.exitCode == 0 {
			t.Fatal("move source remains in HEAD")
		}
		if got := repo.headFile("new.txt"); !bytes.Equal(got, []byte("moved\n")) {
			t.Fatalf("move destination = %q", got)
		}
		if got := repo.indexEntry("other.txt"); got != otherIndex {
			t.Fatalf("unrelated staged entry changed:\nwant %s\n got %s", otherIndex, got)
		}
	})

	t.Run("selecting source includes destination", func(t *testing.T) {
		repo := newRepository(t)
		repo.write("old.txt", []byte("moved\n"))
		repo.git("add", "old.txt")
		repo.git("commit", "-q", "-m", "base")
		if err := os.Rename(
			filepath.Join(repo.dir, "old.txt"),
			filepath.Join(repo.dir, "new.txt"),
		); err != nil {
			t.Fatal(err)
		}

		requireSuccess(t, repo.subcommit(nil, nil, "old.txt", "-m", "move", "--yes"))
		if result := repo.gitResult(nil, "cat-file", "-e", "HEAD:old.txt"); result.exitCode == 0 {
			t.Fatal("move source remains in HEAD")
		}
		if got := repo.headFile("new.txt"); !bytes.Equal(got, []byte("moved\n")) {
			t.Fatalf("move destination = %q", got)
		}
		if got := repo.status(); got != "" {
			t.Fatalf("move did not leave a clean repository: %q", got)
		}
	})

	t.Run("selecting source finds ignored destination", func(t *testing.T) {
		repo := newRepository(t)
		repo.write(".gitignore", []byte("*.ignored\n"))
		repo.write("old.txt", []byte("moved\n"))
		repo.git("add", ".gitignore", "old.txt")
		repo.git("commit", "-q", "-m", "base")
		if err := os.Rename(
			filepath.Join(repo.dir, "old.txt"),
			filepath.Join(repo.dir, "new.ignored"),
		); err != nil {
			t.Fatal(err)
		}

		requireSuccess(t, repo.subcommit(nil, nil, "old.txt", "-m", "move", "--yes"))
		if result := repo.gitResult(nil, "cat-file", "-e", "HEAD:old.txt"); result.exitCode == 0 {
			t.Fatal("ignored move source remains in HEAD")
		}
		if got := repo.headFile("new.ignored"); !bytes.Equal(got, []byte("moved\n")) {
			t.Fatalf("ignored move destination = %q", got)
		}
	})

	t.Run("ambiguous counterpart refuses", func(t *testing.T) {
		repo := newRepository(t)
		for _, path := range []string{"old-a.txt", "old-b.txt"} {
			repo.write(path, []byte("same\n"))
		}
		repo.git("add", "old-a.txt", "old-b.txt")
		repo.git("commit", "-q", "-m", "base")
		repo.remove("old-a.txt")
		repo.remove("old-b.txt")
		repo.write("new.txt", []byte("same\n"))
		before := repo.fingerprint()

		result := repo.subcommit(nil, nil, "new.txt", "-m", "move", "--yes")
		requireRefusal(t, result)
		if !strings.Contains(result.stderr, "cannot determine the exact move counterpart for: new.txt") ||
			!strings.Contains(result.stderr, "old-a.txt") ||
			!strings.Contains(result.stderr, "old-b.txt") {
			t.Fatalf("ambiguous move refusal = %q", result.stderr)
		}
		if got := repo.fingerprint(); got != before {
			t.Fatal("ambiguous move refusal changed repository state")
		}
	})

	t.Run("multiple destinations are ambiguous", func(t *testing.T) {
		repo := newRepository(t)
		repo.write("old.txt", []byte("same\n"))
		repo.git("add", "old.txt")
		repo.git("commit", "-q", "-m", "base")
		repo.remove("old.txt")
		for _, path := range []string{"new-a.txt", "new-b.txt"} {
			repo.write(path, []byte("same\n"))
		}
		before := repo.fingerprint()

		result := repo.subcommit(nil, nil, "new-a.txt", "-m", "move", "--yes")
		requireRefusal(t, result)
		for _, path := range []string{"old.txt", "new-a.txt", "new-b.txt"} {
			if !strings.Contains(result.stderr, path) {
				t.Fatalf("ambiguous destination refusal lacks %s: %q", path, result.stderr)
			}
		}
		if got := repo.fingerprint(); got != before {
			t.Fatal("ambiguous destination refusal changed repository state")
		}
	})

	t.Run("edited move succeeds when both paths are explicit", func(t *testing.T) {
		repo := newRepository(t)
		repo.write("old.txt", []byte("before\n"))
		repo.git("add", "old.txt")
		repo.git("commit", "-q", "-m", "base")
		if err := os.Rename(
			filepath.Join(repo.dir, "old.txt"),
			filepath.Join(repo.dir, "new.txt"),
		); err != nil {
			t.Fatal(err)
		}
		repo.write("new.txt", []byte("after\n"))

		requireSuccess(t, repo.subcommit(
			nil, nil, "old.txt", "new.txt", "-m", "move and edit", "--yes",
		))
		if result := repo.gitResult(nil, "cat-file", "-e", "HEAD:old.txt"); result.exitCode == 0 {
			t.Fatal("edited move source remains in HEAD")
		}
		if got := repo.headFile("new.txt"); !bytes.Equal(got, []byte("after\n")) {
			t.Fatalf("edited move destination = %q", got)
		}
	})
}
