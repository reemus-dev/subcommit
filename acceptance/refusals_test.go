package acceptance

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRefusalsPreserveRepositoryState(t *testing.T) {
	t.Parallel()
	t.Run("active repository operations", func(t *testing.T) {
		cases := []struct {
			name      string
			file      string
			directory string
		}{
			{name: "merge", file: "MERGE_HEAD"},
			{name: "cherry-pick", file: "CHERRY_PICK_HEAD"},
			{name: "revert", file: "REVERT_HEAD"},
			{name: "bisect", file: "BISECT_LOG"},
			{name: "rebase merge", directory: "rebase-merge"},
			{name: "rebase apply", directory: "rebase-apply"},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				repo := newRepository(t)
				repo.seedBasic()
				repo.write("a.txt", []byte("modified\n"))
				beforeHead := repo.head()
				beforeIndex := repo.index()
				beforeWorktree := append([]byte(nil), repo.read("a.txt")...)
				if test.file != "" {
					repo.write(filepath.ToSlash(filepath.Join(".git", test.file)), []byte(beforeHead+"\n"))
				} else if err := os.Mkdir(filepath.Join(repo.dir, ".git", test.directory), 0o755); err != nil {
					t.Fatal(err)
				}

				requireRefusal(t, repo.subcommit(nil, nil, "a.txt", "-m", "refuse", "--yes"))
				assertUnchanged(t, repo, beforeHead, beforeIndex, beforeWorktree, "a.txt")
			})
		}
	})

	t.Run("locked index", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("modified\n"))
		beforeHead := repo.head()
		beforeIndex := repo.index()
		beforeWorktree := append([]byte(nil), repo.read("a.txt")...)
		repo.write(".git/index.lock", nil)

		requireRefusal(t, repo.subcommit(nil, nil, "a.txt", "-m", "refuse", "--yes"))
		assertUnchanged(t, repo, beforeHead, beforeIndex, beforeWorktree, "a.txt")
	})

	t.Run("unresolved HEAD", func(t *testing.T) {
		repo := newRepository(t)
		repo.write("new.txt", []byte("new\n"))
		requireRefusal(t, repo.subcommit(nil, nil, "new.txt", "-m", "refuse", "--yes"))
		if result := repo.gitResult(nil, "rev-parse", "--verify", "HEAD"); result.exitCode == 0 {
			t.Fatal("empty repository unexpectedly acquired HEAD")
		}
	})

	t.Run("noninteractive without yes", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("modified\n"))
		beforeHead := repo.head()

		requireRefusal(t, repo.subcommit(nil, nil, "a.txt", "-m", "refuse"))
		if got := repo.head(); got != beforeHead {
			t.Fatalf("HEAD moved: %s -> %s", beforeHead, got)
		}
	})

	t.Run("directories", func(t *testing.T) {
		for _, test := range []struct {
			name   string
			target string
		}{
			{name: "complete file", target: "target"},
			{name: "range", target: "target:1"},
		} {
			t.Run(test.name, func(t *testing.T) {
				repo := newRepository(t)
				repo.seedBasic()
				if err := os.Mkdir(filepath.Join(repo.dir, "target"), 0o755); err != nil {
					t.Fatal(err)
				}
				before := repo.fingerprint()

				result := repo.subcommit(nil, nil, test.target, "-m", "refuse", "--yes")
				requireRefusal(t, result)
				if !strings.Contains(result.stderr, "cannot commit a directory: target") {
					t.Fatalf("directory refusal = %q", result.stderr)
				}
				if repo.fingerprint() != before {
					t.Fatal("directory refusal changed repository state")
				}
			})
		}
	})

	t.Run("range final symlink", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("creating symlinks requires privileges on Windows")
		}
		repo := newRepository(t)
		repo.seedBasic()
		if err := os.Symlink("a.txt", filepath.Join(repo.dir, "target")); err != nil {
			t.Fatal(err)
		}
		before := repo.fingerprint()

		result := repo.subcommit(nil, nil, "target:1", "-m", "refuse", "--yes")
		requireRefusal(t, result)
		if !strings.Contains(result.stderr, "range selection does not support symbolic links: target") {
			t.Fatalf("symlink refusal = %q", result.stderr)
		}
		if repo.fingerprint() != before {
			t.Fatal("symlink refusal changed repository state")
		}
	})

	t.Run("range deleted target", func(t *testing.T) {
		repo := newRepository(t)
		seedLines(repo, []byte("one\ntwo\nthree\n"))
		repo.remove("f.txt")
		before := repo.fingerprint()

		result := repo.subcommit(nil, nil, "f.txt:2", "-m", "refuse", "--yes")
		requireRefusal(t, result)
		if !strings.Contains(result.stderr, "range selection cannot commit a deleted file: f.txt") ||
			!strings.Contains(result.stderr, "select the complete path without :ranges") {
			t.Fatalf("deleted-target refusal = %q", result.stderr)
		}
		if repo.fingerprint() != before {
			t.Fatal("deleted-target refusal changed repository state")
		}
	})

	t.Run("invalid targets", func(t *testing.T) {
		cases := []struct {
			name string
			args []string
		}{
			{name: "missing file", args: []string{"missing.txt", "-m", "refuse", "--yes"}},
			{name: "untracked range", args: []string{"new.txt:1", "-m", "refuse", "--yes"}},
			{name: "zero range", args: []string{"a.txt:0", "-m", "refuse", "--yes"}},
			{name: "reversed range", args: []string{"a.txt:3-2", "-m", "refuse", "--yes"}},
			{name: "nonnumeric range", args: []string{"a.txt:nope", "-m", "refuse", "--yes"}},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				repo := newRepository(t)
				repo.seedBasic()
				repo.write("a.txt", []byte("modified\n"))
				repo.write("new.txt", []byte("new\n"))
				before := repo.fingerprint()

				requireRefusal(t, repo.subcommit(nil, nil, test.args...))
				if after := repo.fingerprint(); after != before {
					t.Fatal("refusal changed observable repository state")
				}
			})
		}
	})
}

func assertUnchanged(
	t *testing.T, repo *repository,
	head, index string, worktree []byte, path string,
) {
	t.Helper()
	if got := repo.head(); got != head {
		t.Fatalf("HEAD changed: %s -> %s", head, got)
	}
	if got := repo.index(); got != index {
		t.Fatalf("index changed:\nwant %s\n got %s", index, got)
	}
	if got := repo.read(path); !bytes.Equal(got, worktree) {
		t.Fatalf("worktree changed: %q", got)
	}
}
