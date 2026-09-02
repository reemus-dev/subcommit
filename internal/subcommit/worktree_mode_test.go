package subcommit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorktreeModeWithCoreFileModeFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	op := operation{fileMode: false}

	for _, test := range []struct {
		name      string
		headEntry gitEntry
		want      string
	}{
		{name: "non-executable file", headEntry: gitEntry{Mode: "100644", OID: "blob"}, want: "100644"},
		{name: "executable file", headEntry: gitEntry{Mode: "100755", OID: "blob"}, want: "100755"},
		{name: "symlink", headEntry: gitEntry{Mode: "120000", OID: "blob"}, want: "100644"},
		{name: "gitlink", headEntry: gitEntry{Mode: "160000", OID: "commit"}, want: "100644"},
		{name: "untracked", want: "100644"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := op.worktreeMode(info, test.headEntry); got != test.want {
				t.Fatalf("worktreeMode() = %s, want %s", got, test.want)
			}
		})
	}
}
