package acceptance

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompleteFileSymlinks(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires privileges on Windows")
	}

	t.Run("tracked final symlink", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		path := filepath.Join(repo.dir, "target")
		if err := os.Symlink("a.txt", path); err != nil {
			t.Fatal(err)
		}
		repo.git("add", "target")
		repo.git("commit", "-q", "-m", "seed link")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("b.txt", path); err != nil {
			t.Fatal(err)
		}

		requireSuccess(t, repo.subcommit(nil, nil, "target", "-m", "change link", "--yes"))
		if entry := repo.git("ls-tree", "HEAD", "--", "target"); !strings.HasPrefix(entry, "120000 ") {
			t.Fatalf("symlink tree mode = %s", entry)
		}
		if got := repo.headFile("target"); !bytes.Equal(got, []byte("b.txt")) {
			t.Fatalf("symlink blob = %q", got)
		}
	})

	t.Run("untracked final symlink", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		if err := os.Symlink("a.txt", filepath.Join(repo.dir, "target")); err != nil {
			t.Fatal(err)
		}

		requireSuccess(t, repo.subcommit(nil, nil, "target", "-m", "add link", "--yes"))
		if entry := repo.git("ls-tree", "HEAD", "--", "target"); !strings.HasPrefix(entry, "120000 ") {
			t.Fatalf("symlink tree mode = %s", entry)
		}
		if got := repo.headFile("target"); !bytes.Equal(got, []byte("a.txt")) {
			t.Fatalf("symlink blob = %q", got)
		}
	})
}

func TestCompleteFileSymlinkedParent(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires privileges on Windows")
	}

	for _, absolute := range []bool{false, true} {
		name := "relative"
		if absolute {
			name = "absolute"
		}
		t.Run("tracked "+name, func(t *testing.T) {
			repo := newRepository(t)
			repo.write("parent/target.txt", []byte("base\n"))
			repo.git("add", "parent/target.txt")
			repo.git("commit", "-q", "-m", "base")
			if err := os.RemoveAll(filepath.Join(repo.dir, "parent")); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			outsideTarget := filepath.Join(outside, "target.txt")
			if err := os.WriteFile(outsideTarget, []byte("outside\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(repo.dir, "parent")); err != nil {
				t.Fatal(err)
			}
			target := "parent/target.txt"
			if absolute {
				target = filepath.Join(repo.git("rev-parse", "--show-toplevel"), filepath.FromSlash(target))
			}

			requireSuccess(t, repo.subcommit(nil, nil, target, "-m", "outside parent", "--yes"))
			if got := repo.headFile("parent/target.txt"); !bytes.Equal(got, []byte("outside\n")) {
				t.Fatalf("committed content = %q", got)
			}
			if link, err := os.Readlink(filepath.Join(repo.dir, "parent")); err != nil || link != outside {
				t.Fatalf("parent symlink = %q, %v", link, err)
			}
		})

		t.Run("untracked "+name, func(t *testing.T) {
			repo := newRepository(t)
			repo.seedBasic()
			outside := t.TempDir()
			outsideTarget := filepath.Join(outside, "new.txt")
			if err := os.WriteFile(outsideTarget, []byte("outside\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(repo.dir, "parent")); err != nil {
				t.Fatal(err)
			}
			target := "parent/new.txt"
			if absolute {
				target = filepath.Join(repo.git("rev-parse", "--show-toplevel"), filepath.FromSlash(target))
			}
			beforeHead, beforeIndex, beforeStatus := repo.head(), repo.index(), repo.status()

			result := repo.subcommit(nil, nil, target, "-m", "refuse", "--yes")
			requireRefusal(t, result)
			if !strings.Contains(result.stderr, "through a symbolic-link directory") {
				t.Fatalf("symlink-parent refusal = %q", result.stderr)
			}
			if repo.head() != beforeHead || repo.index() != beforeIndex || repo.status() != beforeStatus {
				t.Fatal("symlink-parent refusal changed repository state")
			}
			content, err := os.ReadFile(filepath.Join(outside, "new.txt"))
			if err != nil || !bytes.Equal(content, []byte("outside\n")) {
				t.Fatalf("outside content = %q, %v", content, err)
			}
		})
	}
}

func TestCoreIgnoreCase(t *testing.T) {
	t.Parallel()
	for _, absolute := range []bool{false, true} {
		name := "relative"
		if absolute {
			name = "absolute"
		}
		t.Run("canonicalizes tracked "+name, func(t *testing.T) {
			repo := newRepository(t)
			repo.write("CaseTarget.txt", []byte("base\n"))
			repo.git("add", "CaseTarget.txt")
			repo.git("commit", "-q", "-m", "base")
			repo.git("config", "core.ignorecase", "true")
			repo.write("CaseTarget.txt", []byte("updated\n"))
			target := "casetarget.TXT"
			if absolute {
				root := repo.git("rev-parse", "--show-toplevel")
				target = filepath.Join(swappedCase(root), target)
			}

			requireSuccess(t, repo.subcommit(nil, nil, target, "-m", "case", "--yes"))
			if got := repo.headFile("CaseTarget.txt"); !bytes.Equal(got, []byte("updated\n")) {
				t.Fatalf("canonical entry content = %q", got)
			}
			var matches []string
			for _, path := range strings.Split(repo.git("ls-tree", "-r", "--name-only", "HEAD"), "\n") {
				if strings.EqualFold(path, "CaseTarget.txt") {
					matches = append(matches, path)
				}
			}
			if len(matches) != 1 || matches[0] != "CaseTarget.txt" {
				t.Fatalf("case-variant tree entries = %q", matches)
			}
		})
	}

	t.Run("false preserves distinct literal case", func(t *testing.T) {
		repo := newRepository(t)
		probe := filepath.Join(repo.dir, "CaseProbe")
		if err := os.WriteFile(probe, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(repo.dir, "caseprobe")); err == nil {
			t.Skip("filesystem is case-insensitive")
		}
		if err := os.Remove(probe); err != nil {
			t.Fatal(err)
		}
		repo.write("CaseTarget.txt", []byte("upper\n"))
		repo.git("add", "CaseTarget.txt")
		repo.git("commit", "-q", "-m", "base")
		repo.git("config", "core.ignorecase", "false")
		repo.write("casetarget.txt", []byte("lower\n"))

		requireSuccess(t, repo.subcommit(nil, nil, "casetarget.txt", "-m", "distinct", "--yes"))
		for path, want := range map[string][]byte{
			"CaseTarget.txt": []byte("upper\n"),
			"casetarget.txt": []byte("lower\n"),
		} {
			if got := repo.headFile(path); !bytes.Equal(got, want) {
				t.Fatalf("HEAD:%s = %q", path, got)
			}
		}
	})
}

func swappedCase(value string) string {
	upper := strings.ToUpper(value)
	if upper != value {
		return upper
	}
	return strings.ToLower(value)
}

func TestLiteralPathspecMagic(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("pathspec magic characters are not valid in Windows filenames")
	}

	targets := []string{"star*.txt", "question?.txt", "bracket[ab].txt", ":(glob)literal.txt"}
	matches := []string{"star-one.txt", "question1.txt", "bracketa.txt", "literal.txt"}
	env := map[string]string{
		"GIT_INDEX_FILE":        filepath.Join(t.TempDir(), "inherited-index"),
		"GIT_GLOB_PATHSPECS":    "1",
		"GIT_ICASE_PATHSPECS":   "1",
		"GIT_LITERAL_PATHSPECS": "0",
		"GIT_NOGLOB_PATHSPECS":  "1",
	}

	t.Run("complete files", func(t *testing.T) {
		repo := newRepository(t)
		for _, path := range append(targets, matches...) {
			repo.write(path, []byte("base\n"))
		}
		repo.git("add", "-A")
		repo.git("commit", "-q", "-m", "seed magic names")
		for _, path := range matches {
			repo.write(path, []byte("matching staged\n"))
		}
		repo.git("add", "-A")
		for _, path := range targets {
			repo.write(path, []byte("target worktree\n"))
		}
		for _, path := range matches {
			repo.write(path, []byte("matching worktree\n"))
		}
		matchingIndex := make(map[string]string, len(matches))
		for _, path := range matches {
			matchingIndex[path] = repo.indexEntry(path)
		}

		args := append([]string(nil), targets...)
		args = append(args, "-m", "literal complete files", "--yes")
		requireSuccess(t, repo.subcommit(nil, env, args...))

		for _, path := range targets {
			if got := repo.headFile(path); !bytes.Equal(got, []byte("target worktree\n")) {
				t.Fatalf("HEAD:%s = %q", path, got)
			}
		}
		for _, path := range matches {
			if got := repo.headFile(path); !bytes.Equal(got, []byte("base\n")) {
				t.Fatalf("matching HEAD:%s changed to %q", path, got)
			}
			if got := repo.read(path); !bytes.Equal(got, []byte("matching worktree\n")) {
				t.Fatalf("matching worktree %s changed to %q", path, got)
			}
		}
		for _, path := range matches {
			if got := repo.indexEntry(path); got != matchingIndex[path] {
				t.Fatalf("matching index entry %s changed:\nwant %s\n got %s", path, matchingIndex[path], got)
			}
		}
	})

	t.Run("ranges", func(t *testing.T) {
		repo := newRepository(t)
		for _, path := range append(targets, matches...) {
			repo.write(path, []byte("one\ntwo\nthree\n"))
		}
		repo.git("add", "-A")
		repo.git("commit", "-q", "-m", "seed magic names")
		for _, path := range matches {
			repo.write(path, []byte("one\nMATCHING STAGED\nthree\n"))
		}
		repo.git("add", "-A")
		for _, path := range targets {
			repo.write(path, []byte("one\nTARGET\nthree\n"))
		}
		for _, path := range matches {
			repo.write(path, []byte("one\nMATCHING STAGED\nMATCHING WORKTREE\n"))
		}
		matchingIndex := make(map[string]string, len(matches))
		for _, path := range matches {
			matchingIndex[path] = repo.indexEntry(path)
		}

		var args []string
		for _, path := range targets {
			args = append(args, path+":2")
		}
		args = append(args, "-m", "literal ranges", "--yes")
		requireSuccess(t, repo.subcommit(nil, env, args...))

		targetHead := []byte("one\nTARGET\nthree\n")
		for _, path := range targets {
			if got := repo.headFile(path); !bytes.Equal(got, targetHead) {
				t.Fatalf("HEAD:%s = %q", path, got)
			}
		}
		matchingHead := []byte("one\ntwo\nthree\n")
		matchingWorktree := []byte("one\nMATCHING STAGED\nMATCHING WORKTREE\n")
		for _, path := range matches {
			if got := repo.headFile(path); !bytes.Equal(got, matchingHead) {
				t.Fatalf("matching HEAD:%s changed to %q", path, got)
			}
			if got := repo.read(path); !bytes.Equal(got, matchingWorktree) {
				t.Fatalf("matching worktree %s changed to %q", path, got)
			}
		}
		for _, path := range matches {
			if got := repo.indexEntry(path); got != matchingIndex[path] {
				t.Fatalf("matching index entry %s changed:\nwant %s\n got %s", path, matchingIndex[path], got)
			}
		}
	})
}

func TestReservedLookingCompletePaths(t *testing.T) {
	t.Parallel()
	t.Run("completion is an ordinary path", func(t *testing.T) {
		repo := newRepository(t)
		repo.write("completion", []byte("base\n"))
		repo.git("add", "completion")
		repo.git("commit", "-q", "-m", "base")
		repo.write("completion", []byte("changed\n"))

		requireSuccess(t, repo.subcommit(
			nil, nil, "completion", "-m", "update completion", "--yes",
		))
		if got := repo.headFile("completion"); !bytes.Equal(got, []byte("changed\n")) {
			t.Fatalf("HEAD:completion = %q", got)
		}
	})

	t.Run("complete flag escapes range syntax", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("colon is not valid in Windows filenames")
		}
		repo := newRepository(t)
		repo.write("report:42", []byte("base\n"))
		repo.git("add", "report:42")
		repo.git("commit", "-q", "-m", "base")
		repo.write("report:42", []byte("changed\n"))

		requireSuccess(t, repo.subcommit(
			nil, nil, "--complete", "report:42", "-m", "update report", "--yes",
		))
		if got := repo.headFile("report:42"); !bytes.Equal(got, []byte("changed\n")) {
			t.Fatalf("HEAD:report:42 = %q", got)
		}
	})
}
