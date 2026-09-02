package acceptance

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
)

func TestRangeSelectionAndDiffEdges(t *testing.T) {
	t.Run("commits selected region and preserves separated edit", func(t *testing.T) {
		repo := newRepository(t)
		seedLines(repo, []byte("l1\nl2\nl3\nl4\nl5\n"))
		repo.write("f.txt", []byte("l1\nL2\nl3\nL4\nl5\n"))
		repo.write("other.txt", []byte("untracked\n"))

		result := repo.subcommit(nil, nil, "f.txt:2", "-m", "line two", "--yes")
		requireSuccess(t, result)
		if !strings.Contains(result.stderr, "Selected regions") ||
			!strings.Contains(result.stderr, "+L2") ||
			strings.Contains(result.stderr, "+L4") {
			t.Fatalf("effective range patch = %q", result.stderr)
		}
		if got := repo.headFile("f.txt"); !bytes.Equal(got, []byte("l1\nL2\nl3\nl4\nl5\n")) {
			t.Fatalf("committed content = %q", got)
		}
		if got := repo.read("f.txt"); !bytes.Equal(got, []byte("l1\nL2\nl3\nL4\nl5\n")) {
			t.Fatalf("worktree content changed = %q", got)
		}
		status := repo.status()
		if !strings.Contains(status, " M f.txt") || !strings.Contains(status, "?? other.txt") {
			t.Fatalf("preserved status missing:\n%s", status)
		}
	})

	t.Run("refuses atomically when one requested range misses", func(t *testing.T) {
		repo := newRepository(t)
		base := []byte("one\ntwo\nthree\nfour\nfive\n")
		for _, path := range []string{"missed.txt", "applied.txt"} {
			repo.write(path, base)
		}
		repo.git("add", "missed.txt", "applied.txt")
		repo.git("commit", "-q", "-m", "line bases")
		repo.write("missed.txt", []byte("one\nSTAGED\nthree\nfour\nfive\n"))
		repo.git("add", "missed.txt")
		repo.write("missed.txt", []byte("one\nSTAGED\nWORKTREE\nfour\nfive\n"))
		repo.write("applied.txt", []byte("one\nAPPLIED\nthree\nfour\nfive\n"))
		before := repo.fingerprint()

		result := repo.subcommit(
			nil, nil, "missed.txt:5", "applied.txt:2",
			"-m", "apply one target", "--yes", "--quiet",
		)
		requireRefusal(t, result)
		if !strings.Contains(result.stderr, "not every selected target contains a committable change") ||
			!strings.Contains(result.stderr, "missed.txt  selected ranges do not overlap changed hunks") {
			t.Fatalf("missed-range refusal absent:\n%s", result.stderr)
		}
		if got := repo.fingerprint(); got != before {
			t.Fatal("missed-range refusal changed repository state")
		}
	})

	t.Run("ignores staged mode with core.fileMode false", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows worktrees do not expose Unix executable mode")
		}
		repo := newRepository(t)
		seedLines(repo, []byte("one\ntwo\nthree\n"))
		repo.git("config", "core.fileMode", "false")
		repo.git("update-index", "--chmod=+x", "f.txt")
		repo.write("f.txt", []byte("one\nTWO\nthree\n"))

		requireSuccess(t, repo.subcommit(nil, nil, "f.txt:2", "-m", "line two", "--yes"))
		if entry := repo.git("ls-tree", "HEAD", "--", "f.txt"); !strings.HasPrefix(entry, "100644 ") {
			t.Fatalf("HEAD mode = %s", entry)
		}
		if entry := repo.indexEntry("f.txt"); !strings.HasPrefix(entry, "100644 ") {
			t.Fatalf("selected index mode = %s", entry)
		}
		if got := repo.headFile("f.txt"); !bytes.Equal(got, []byte("one\nTWO\nthree\n")) {
			t.Fatalf("HEAD:f.txt = %q", got)
		}
	})

	t.Run("sees present skip-worktree target", func(t *testing.T) {
		repo := newRepository(t)
		seedLines(repo, []byte("one\ntwo\nthree\n"))
		repo.git("update-index", "--skip-worktree", "f.txt")
		repo.write("f.txt", []byte("one\nTWO\nthree\n"))

		requireSuccess(t, repo.subcommit(nil, nil, "f.txt:2", "-m", "line two", "--yes"))
		if got := repo.headFile("f.txt"); !bytes.Equal(got, []byte("one\nTWO\nthree\n")) {
			t.Fatalf("HEAD:f.txt = %q", got)
		}
		if result := repo.gitResult(
			nil, "diff", "--cached", "--quiet", "HEAD", "--", "f.txt",
		); result.exitCode != 0 {
			t.Fatalf("selected index does not equal HEAD: %s", result.output())
		}
	})

	base := []byte("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n")
	staged := []byte("1\nTWO\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n")
	worktree := []byte("1\nTWO\n3\n4\n5\n6\n7\n8\n9\n10\nELEVEN\n12\n")
	for _, test := range []struct {
		name     string
		target   string
		message  string
		wantHead []byte
	}{
		{
			name:     "commits a previously staged selected region",
			target:   "f.txt:2",
			message:  "line two",
			wantHead: staged,
		},
		{
			name:     "supersedes a previously staged unselected region",
			target:   "f.txt:11",
			message:  "line eleven",
			wantHead: []byte("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\nELEVEN\n12\n"),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newRepository(t)
			seedLines(repo, base)
			repo.write("f.txt", staged)
			repo.git("add", "f.txt")
			repo.write("f.txt", worktree)
			repo.write("b.txt", []byte("unrelated staged\n"))
			repo.git("add", "b.txt")
			unrelatedIndex := repo.indexEntry("b.txt")

			requireSuccess(t, repo.subcommit(
				nil, nil, test.target,
				"-m", test.message, "--yes",
			))
			if got := repo.headFile("f.txt"); !bytes.Equal(got, test.wantHead) {
				t.Fatalf("HEAD:f.txt = %q", got)
			}
			if got := repo.read("f.txt"); !bytes.Equal(got, worktree) {
				t.Fatalf("worktree f.txt changed to %q", got)
			}
			if result := repo.gitResult(
				nil, "diff", "--cached", "--quiet", "HEAD", "--", "f.txt",
			); result.exitCode != 0 {
				t.Fatalf("selected index does not equal HEAD: %s", result.output())
			}
			if got := repo.indexEntry("b.txt"); got != unrelatedIndex {
				t.Fatalf("unrelated staged entry changed:\nwant %s\n got %s", unrelatedIndex, got)
			}
			if status := repo.status(); !strings.Contains(status, " M f.txt") {
				t.Fatalf("unselected worktree region is not unstaged:\n%s", status)
			}
		})
	}

	t.Run("includes whole contiguous change region", func(t *testing.T) {
		repo := newRepository(t)
		seedLines(repo, []byte("a\nb\nc\nd\ne\n"))
		repo.write("f.txt", []byte("a\nB\nC\nCC\nd\ne\n"))

		result := repo.subcommit(nil, nil, "f.txt:2", "-m", "region", "--yes")
		requireSuccess(t, result)
		for _, line := range []string{"+B", "+C", "+CC"} {
			if !strings.Contains(result.stderr, line) {
				t.Fatalf("effective region preview lacks %q: %s", line, result.stderr)
			}
		}
		if got := repo.headFile("f.txt"); !bytes.Equal(got, []byte("a\nB\nC\nCC\nd\ne\n")) {
			t.Fatalf("whole region not committed: %q", got)
		}
	})

	t.Run("accepts comma separated ranges across hunks", func(t *testing.T) {
		repo := newRepository(t)
		seedLines(repo, []byte("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n"))
		repo.write("f.txt", []byte("ONE\n2\n3\n4\n5\n6\n7\n8\n9\nTEN\n"))

		requireSuccess(t, repo.subcommit(nil, nil, "f.txt:1,10", "-m", "two hunks", "--yes"))
		if got := repo.headFile("f.txt"); !bytes.Equal(got, repo.read("f.txt")) {
			t.Fatalf("selected hunks differ from worktree: %q", got)
		}
	})

	t.Run("preserves excluded no-final-newline marker", func(t *testing.T) {
		repo := newRepository(t)
		seedLines(repo, []byte("line1\nmid\nline3"))
		repo.write("f.txt", []byte("LINE1\nmid\nLINE3"))

		requireSuccess(t, repo.subcommit(nil, nil, "f.txt:1", "-m", "first", "--yes"))
		if got := repo.headFile("f.txt"); !bytes.Equal(got, []byte("LINE1\nmid\nline3")) {
			t.Fatalf("committed no-newline content = %q", got)
		}
		if got := repo.read("f.txt"); !bytes.Equal(got, []byte("LINE1\nmid\nLINE3")) {
			t.Fatalf("excluded edit changed = %q", got)
		}
	})

	t.Run("selects pure deletion at EOF", func(t *testing.T) {
		repo := newRepository(t)
		seedLines(repo, []byte("a\nb\nc\nd\ne\n"))
		repo.write("f.txt", []byte("a\nb\nc\n"))

		requireSuccess(t, repo.subcommit(nil, nil, "f.txt:3", "-m", "delete tail", "--yes"))
		if got := repo.headFile("f.txt"); !bytes.Equal(got, []byte("a\nb\nc\n")) {
			t.Fatalf("EOF deletion result = %q", got)
		}
	})

	t.Run("preserves CRLF", func(t *testing.T) {
		repo := newRepository(t)
		seedLines(repo, []byte("one\r\ntwo\r\nthree\r\n"))
		repo.write("f.txt", []byte("one\r\nTWO\r\nthree\r\n"))

		requireSuccess(t, repo.subcommit(nil, nil, "f.txt:2", "-m", "line two", "--yes"))
		if got := repo.headFile("f.txt"); !bytes.Equal(got, []byte("one\r\nTWO\r\nthree\r\n")) {
			t.Fatalf("CRLF content changed = %q", got)
		}
	})
}

func seedLines(repo *repository, content []byte) {
	repo.t.Helper()
	repo.write("f.txt", content)
	repo.git("add", "f.txt")
	repo.git("commit", "-q", "-m", "seed lines")
}
