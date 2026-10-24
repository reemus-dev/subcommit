package acceptance

import (
	"bytes"
	"os"
	"path/filepath"
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

	t.Run("ignored identical file is not a destination", func(t *testing.T) {
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

		requireSuccess(t, repo.subcommit(nil, nil, "old.txt", "-m", "delete", "--yes"))
		if result := repo.gitResult(nil, "cat-file", "-e", "HEAD:old.txt"); result.exitCode == 0 {
			t.Fatal("deleted source remains in HEAD")
		}
		if result := repo.gitResult(nil, "cat-file", "-e", "HEAD:new.ignored"); result.exitCode == 0 {
			t.Fatal("ignored file was committed")
		}
	})

	t.Run("multiple sources commit only the selected destination", func(t *testing.T) {
		repo := newRepository(t)
		for _, path := range []string{"old-a.txt", "old-b.txt"} {
			repo.write(path, []byte("same\n"))
		}
		repo.git("add", "old-a.txt", "old-b.txt")
		repo.git("commit", "-q", "-m", "base")
		repo.remove("old-a.txt")
		repo.remove("old-b.txt")
		repo.write("new.txt", []byte("same\n"))

		requireSuccess(t, repo.subcommit(nil, nil, "new.txt", "-m", "add", "--yes"))
		if got := repo.headFile("new.txt"); !bytes.Equal(got, []byte("same\n")) {
			t.Fatalf("selected destination = %q", got)
		}
		for _, path := range []string{"old-a.txt", "old-b.txt"} {
			if result := repo.gitResult(nil, "cat-file", "-e", "HEAD:"+path); result.exitCode != 0 {
				t.Fatalf("unselected source %s was deleted", path)
			}
		}
	})

	t.Run("multiple destinations commit only the selected source", func(t *testing.T) {
		repo := newRepository(t)
		repo.write("old.txt", []byte("same\n"))
		repo.git("add", "old.txt")
		repo.git("commit", "-q", "-m", "base")
		repo.remove("old.txt")
		for _, path := range []string{"new-a.txt", "new-b.txt"} {
			repo.write(path, []byte("same\n"))
		}

		requireSuccess(t, repo.subcommit(nil, nil, "old.txt", "-m", "delete", "--yes"))
		if result := repo.gitResult(nil, "cat-file", "-e", "HEAD:old.txt"); result.exitCode == 0 {
			t.Fatal("selected source remains in HEAD")
		}
		for _, path := range []string{"new-a.txt", "new-b.txt"} {
			if result := repo.gitResult(nil, "cat-file", "-e", "HEAD:"+path); result.exitCode == 0 {
				t.Fatalf("unselected destination %s was committed", path)
			}
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
