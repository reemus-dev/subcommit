package subcommit

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reemus-dev/subcommit/internal/console"
)

func TestOrphanCandidateErrorPreservesDiagnosticAndAddsRecovery(t *testing.T) {
	cause := &console.Diagnostic{
		Summary: "repository changed",
		Hint:    "inspect the latest commit",
	}
	commit := strings.Repeat("a", 40)

	err := orphanCandidateError(cause, commit)
	var diagnostic *console.Diagnostic
	if !errors.As(err, &diagnostic) {
		t.Fatalf("error type = %T", err)
	}
	if diagnostic.Summary != cause.Summary || diagnostic.Hint != cause.Hint {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}
	if !diagnosticContains(diagnostic, "Candidate commit", commit) ||
		!diagnosticContains(diagnostic, "Recovery", "git cherry-pick "+commit) {
		t.Fatalf("recovery diagnostic = %#v", diagnostic)
	}
}

func TestPublicationFailureExplainsStateAndRecovery(t *testing.T) {
	transactionDir := filepath.Join("tmp", "subcommit-transaction")
	op := &operation{transactionDir: transactionDir}
	commit := strings.Repeat("b", 40)
	lockPath := filepath.Join("repo", ".git", "index.lock")

	err := op.publicationFailure(commit, lockPath, errors.New("sharing violation"))
	var diagnostic *console.Diagnostic
	if !errors.As(err, &diagnostic) {
		t.Fatalf("error type = %T", err)
	}
	if !diagnosticContains(diagnostic, "Commit", commit) ||
		!diagnosticContains(diagnostic, "Recovery files", lockPath) ||
		!diagnosticContains(diagnostic, "Recovery", "git reset --mixed "+commit) ||
		!diagnosticContains(diagnostic, "Cause", "sharing violation") {
		t.Fatalf("publication diagnostic = %#v", diagnostic)
	}
}

func diagnosticContains(diagnostic *console.Diagnostic, title, fragment string) bool {
	for _, section := range diagnostic.Sections {
		if section.Title != title {
			continue
		}
		for _, line := range section.Lines {
			if strings.Contains(line, fragment) {
				return true
			}
		}
	}
	return false
}
