package acceptance

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCoreFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows worktrees do not expose Unix executable mode")
	}

	t.Run("false keeps HEAD mode for tracked content", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		if err := os.Chmod(filepath.Join(repo.dir, "a.txt"), 0o755); err != nil {
			t.Fatal(err)
		}
		repo.git("add", "a.txt")
		repo.git("commit", "-q", "-m", "executable base")
		repo.git("config", "core.fileMode", "false")
		if err := os.Chmod(filepath.Join(repo.dir, "a.txt"), 0o644); err != nil {
			t.Fatal(err)
		}
		repo.write("a.txt", []byte("updated\n"))

		requireSuccess(t, repo.subcommit(nil, nil, "a.txt", "-m", "update content", "--yes"))
		if entry := repo.git("ls-tree", "HEAD", "--", "a.txt"); !strings.HasPrefix(entry, "100755 ") {
			t.Fatalf("HEAD mode = %s", entry)
		}
		if got := repo.headFile("a.txt"); !bytes.Equal(got, []byte("updated\n")) {
			t.Fatalf("HEAD content = %q", got)
		}
	})

	t.Run("false uses non-executable mode for untracked file", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.git("config", "core.fileMode", "false")
		repo.write("new.txt", []byte("new\n"))
		if err := os.Chmod(filepath.Join(repo.dir, "new.txt"), 0o755); err != nil {
			t.Fatal(err)
		}

		requireSuccess(t, repo.subcommit(nil, nil, "new.txt", "-m", "add file", "--yes"))
		if entry := repo.git("ls-tree", "HEAD", "--", "new.txt"); !strings.HasPrefix(entry, "100644 ") {
			t.Fatalf("HEAD mode = %s", entry)
		}
	})

	t.Run("false aligns selected index with committed mode", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.git("config", "core.fileMode", "false")
		repo.git("update-index", "--chmod=+x", "a.txt")
		repo.write("a.txt", []byte("updated\n"))

		requireSuccess(t, repo.subcommit(nil, nil, "a.txt", "-m", "update content", "--yes"))
		headEntry := repo.git("ls-tree", "HEAD", "--", "a.txt")
		if !strings.HasPrefix(headEntry, "100644 ") {
			t.Fatalf("HEAD mode = %s", headEntry)
		}
		indexEntry := repo.indexEntry("a.txt")
		if !strings.HasPrefix(indexEntry, "100644 ") {
			t.Fatalf("published index retained staged mode: %s", indexEntry)
		}
		headBlob := strings.Fields(headEntry)[2]
		if indexBlob := strings.Fields(indexEntry)[1]; indexBlob != headBlob {
			t.Fatalf("index blob = %s, want committed blob %s", indexBlob, headBlob)
		}
	})

	t.Run("false changes symlink to regular file like native Git", func(t *testing.T) {
		nativeRepo := newRepository(t)
		helperRepo := newRepository(t)
		for _, repo := range []*repository{nativeRepo, helperRepo} {
			repo.seedBasic()
			path := filepath.Join(repo.dir, "a.txt")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("b.txt", path); err != nil {
				t.Fatal(err)
			}
			repo.git("add", "a.txt")
			repo.git("commit", "-q", "-m", "symlink base")
			repo.git("config", "core.fileMode", "false")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			repo.write("a.txt", []byte("regular\n"))
		}

		nativeRepo.git("commit", "-q", "-m", "typechange", "--", "a.txt")
		requireSuccess(t, helperRepo.subcommit(nil, nil, "a.txt", "-m", "typechange", "--yes"))
		nativeEntry := nativeRepo.git("ls-tree", "HEAD", "--", "a.txt")
		helperEntry := helperRepo.git("ls-tree", "HEAD", "--", "a.txt")
		if nativeEntry != helperEntry || !strings.HasPrefix(helperEntry, "100644 ") {
			t.Fatalf("typechange entries differ:\nnative %s\nhelper %s", nativeEntry, helperEntry)
		}
		if entry := helperRepo.indexEntry("a.txt"); !strings.HasPrefix(entry, "100644 ") {
			t.Fatalf("published index mode = %s", entry)
		}
	})

	t.Run("false ignores filesystem-only chmod", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.git("config", "core.fileMode", "false")
		if err := os.Chmod(filepath.Join(repo.dir, "a.txt"), 0o755); err != nil {
			t.Fatal(err)
		}
		beforeHead, beforeIndex := repo.head(), repo.indexEntry("a.txt")

		result := repo.subcommit(nil, nil, "a.txt", "-m", "ignored mode", "--yes")
		requireRefusal(t, result)
		if !strings.Contains(result.stderr, "error: nothing to commit") ||
			!strings.Contains(result.stderr, "no changes relative to the current commit") {
			t.Fatalf("no-change diagnostic missing: %s", result.stderr)
		}
		if repo.head() != beforeHead || repo.indexEntry("a.txt") != beforeIndex {
			t.Fatal("ignored chmod changed HEAD or index")
		}
	})
}

func TestGPGSigningConfigurationIsHonored(t *testing.T) {
	t.Run("absent is unsigned", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("unsigned candidate\n"))
		repo.git("config", "--unset", "commit.gpgsign")
		repo.git("config", "gpg.program", hookHelper)

		requireSuccess(t, repo.subcommit(
			nil, nil, "a.txt", "-m", "unsigned", "--yes", "--no-verify",
		))
	})

	t.Run("true signs", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("signed candidate\n"))
		repo.git("config", "commit.gpgsign", "true")
		repo.git("config", "gpg.program", hookHelper)
		beforeHead, beforeIndex := repo.head(), repo.index()

		result := repo.subcommit(nil, nil, "a.txt", "-m", "must sign", "--yes", "--no-verify")
		requireRefusal(t, result)
		if repo.head() != beforeHead || repo.index() != beforeIndex {
			t.Fatal("failed signing attempt changed HEAD or index")
		}
	})

	t.Run("malformed refuses without mutation", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("candidate\n"))
		repo.write("b.txt", []byte("unrelated staged\n"))
		repo.git("add", "b.txt")
		repo.write("b.txt", []byte("unrelated worktree\n"))
		repo.git("config", "commit.gpgsign", "malformed")
		before := repo.fingerprint()

		result := repo.subcommit(nil, nil, "a.txt", "-m", "refuse", "--yes", "--no-verify")
		requireRefusal(t, result)
		if !strings.Contains(result.stderr, `invalid boolean value "malformed"`) ||
			!strings.Contains(result.stderr, "set commit.gpgsign to true or false") {
			t.Fatalf("malformed configuration diagnostic missing: %s", result.stderr)
		}
		if after := repo.fingerprint(); after != before {
			t.Fatal("malformed signing configuration changed repository state")
		}
	})
}
