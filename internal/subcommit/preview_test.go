package subcommit

import (
	"strings"
	"testing"

	"github.com/reemus-dev/subcommit/internal/console"
)

func TestSafePreviewPathQuotesUnsafeBytes(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "ordinary path.txt", want: "ordinary path.txt"},
		{path: "line\nbreak.txt", want: `"line\nbreak.txt"`},
		{path: "tab\tpath.txt", want: `"tab\tpath.txt"`},
		{path: string([]byte{'b', 'a', 'd', 0xff}), want: `"bad\xff"`},
	}
	for _, test := range tests {
		if got := safePreviewPath(test.path); got != test.want {
			t.Errorf("safePreviewPath(%q) = %q, want %q", test.path, got, test.want)
		}
	}
}

func TestNothingToCommitDiagnosticQuotesSkippedPath(t *testing.T) {
	err := nothingToCommitError([]skippedPath{{
		path:   "line\nbreak.txt",
		reason: "no changes",
	}})
	if !strings.Contains(err.(*console.Diagnostic).Sections[0].Lines[0], `"line\nbreak.txt"`) {
		t.Fatalf("diagnostic = %#v", err)
	}
}
