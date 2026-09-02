package acceptance

import (
	"fmt"
	"strings"
	"testing"
)

func TestColorModes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		color     string
		env       map[string]string
		wantColor bool
	}{
		{name: "auto disables color when redirected", color: "auto"},
		{name: "never disables color", color: "never"},
		{
			name:      "always forces color despite NO_COLOR",
			color:     "always",
			env:       map[string]string{"NO_COLOR": "1", "TERM": "dumb"},
			wantColor: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newRepository(t)
			repo.seedBasic()
			repo.git("config", "color.ui", "always")
			repo.write("a.txt", []byte("modified\n"))

			result := repo.subcommit(
				nil, test.env, "a.txt", "-m", "message", "--yes",
				"--color="+test.color,
			)
			requireSuccess(t, result)
			if got := strings.Contains(result.stderr, "\x1b["); got != test.wantColor {
				t.Fatalf("stderr color = %t, want %t: %q", got, test.wantColor, result.stderr)
			}
			if strings.Contains(result.stdout, "\x1b[") {
				t.Fatalf("stable success record contains color: %q", result.stdout)
			}
		})
	}

	t.Run("range selection ignores forced Git diff color", func(t *testing.T) {
		repo := newRepository(t)
		seedLines(repo, []byte("one\ntwo\nthree\n"))
		repo.git("config", "color.ui", "always")
		repo.write("f.txt", []byte("one\nTWO\nthree\n"))

		result := repo.subcommit(
			nil, nil, "f.txt:2", "-m", "line", "--yes", "--color=never",
		)
		requireSuccess(t, result)
		if got := repo.headFile("f.txt"); string(got) != "one\nTWO\nthree\n" {
			t.Fatalf("committed lines content = %q", got)
		}
	})
}

func TestSelectedUntrackedDirectoryIsNotReportedAsPreserved(t *testing.T) {
	t.Parallel()
	repo := newRepository(t)
	repo.seedBasic()
	repo.write("new/one.txt", []byte("one\n"))
	repo.write("new/two.txt", []byte("two\n"))

	result := repo.subcommit(
		nil, nil, "new/one.txt", "new/two.txt", "-m", "new files", "--yes",
	)
	requireSuccess(t, result)
	if strings.Contains(result.stderr, "Other changes") ||
		strings.Contains(result.stderr, "?? new/") {
		t.Fatalf("selected untracked directory reported as preserved: %q", result.stderr)
	}
}

func TestQuietAndVerboseOutput(t *testing.T) {
	t.Parallel()
	t.Run("quiet keeps only stable success output", func(t *testing.T) {
		repo := outputRepository(t)
		result := repo.subcommit(
			nil, nil, "a.txt", "-m", "message", "--yes", "--quiet",
		)
		requireSuccess(t, result)
		if result.stderr != "" {
			t.Fatalf("quiet stderr = %q", result.stderr)
		}
	})

	t.Run("default caps preserved paths", func(t *testing.T) {
		repo := outputRepository(t)
		result := repo.subcommit(nil, nil, "a.txt", "-m", "message", "--yes")
		requireSuccess(t, result)
		if !strings.Contains(result.stderr, "Other changes (13 paths, not committed)") ||
			!strings.Contains(result.stderr, "... 1 more. Use --verbose to list all") {
			t.Fatalf("default preserved output = %q", result.stderr)
		}
	})

	t.Run("verbose shows every preserved path", func(t *testing.T) {
		repo := outputRepository(t)
		result := repo.subcommit(
			nil, nil, "a.txt", "-m", "message", "--yes", "--verbose",
		)
		requireSuccess(t, result)
		if !strings.Contains(result.stderr, "?? untracked-12.txt") ||
			strings.Contains(result.stderr, "more. Use --verbose") {
			t.Fatalf("verbose preserved output = %q", result.stderr)
		}
	})
}

func outputRepository(t *testing.T) *repository {
	t.Helper()
	repo := newRepository(t)
	repo.seedBasic()
	repo.write("a.txt", []byte("modified\n"))
	for index := 0; index < 13; index++ {
		repo.write(fmt.Sprintf("untracked-%02d.txt", index), []byte("untracked\n"))
	}
	return repo
}
