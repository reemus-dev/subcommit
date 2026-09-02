package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitMessageSources(t *testing.T) {
	t.Run("reads multiline message from stdin", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("modified\n"))
		message := []byte("subject\n\nbody from stdin\n")

		result := repo.subcommit(
			message, nil, "a.txt", "--file=-", "--yes",
		)
		requireSuccess(t, result)
		if got := repo.git("log", "-1", "--pretty=%B"); got != strings.TrimSpace(string(message)) {
			t.Fatalf("commit message = %q", got)
		}
		if !strings.Contains(result.stderr, "body from stdin") {
			t.Fatalf("preview does not contain stdin message: %q", result.stderr)
		}
	})

	t.Run("reads message from relative file", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("modified\n"))
		messagePath := filepath.Join(repo.dir, ".git", "subcommit-message")
		if err := os.WriteFile(messagePath, []byte("message from file\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		result := repo.subcommit(
			nil, nil, "a.txt", "-F", ".git/subcommit-message", "--yes",
		)
		requireSuccess(t, result)
		if got := repo.git("log", "-1", "--pretty=%B"); got != "message from file" {
			t.Fatalf("commit message = %q", got)
		}
	})

	t.Run("refuses empty stdin without mutation", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("modified\n"))
		before := repo.fingerprint()

		result := repo.subcommit(
			[]byte(" \n\t\n"), nil, "a.txt", "-F", "-", "--yes",
		)
		requireRefusal(t, result)
		if !strings.Contains(result.stderr, "commit message cannot be empty") {
			t.Fatalf("empty-message diagnostic = %q", result.stderr)
		}
		if got := repo.fingerprint(); got != before {
			t.Fatalf("empty message changed repository:\nwant %s\n got %s", before, got)
		}
	})

	t.Run("reports unreadable message file without mutation", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("modified\n"))
		before := repo.fingerprint()

		result := repo.subcommit(
			nil, nil, "a.txt", "-F", ".git/missing-message", "--yes",
		)
		requireRefusal(t, result)
		if !strings.Contains(result.stderr, "cannot read commit message from file") ||
			!strings.Contains(result.stderr, ".git/missing-message") {
			t.Fatalf("message-file diagnostic = %q", result.stderr)
		}
		if got := repo.fingerprint(); got != before {
			t.Fatalf("message file failure changed repository:\nwant %s\n got %s", before, got)
		}
	})
}
