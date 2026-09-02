package subcommit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNormalizePath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub", "space ü"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "CaseDir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "CaseDir", "Tracked.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "absolute.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	outsideRoot := t.TempDir()
	outside := filepath.Join(outsideRoot, "outside.txt")
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outsideRoot, filepath.Join(root, "linked-parent")); err != nil {
			t.Fatal(err)
		}
	} else if err := os.Mkdir(filepath.Join(root, "linked-parent"), 0o755); err != nil {
		t.Fatal(err)
	}

	wrongCaseRoot := filepath.Join(filepath.Dir(root), strings.ToUpper(filepath.Base(root)))
	if wrongCaseRoot == root {
		wrongCaseRoot = filepath.Join(filepath.Dir(root), strings.ToLower(filepath.Base(root)))
	}
	tests := []struct {
		name       string
		prefix     string
		value      string
		ignoreCase bool
		want       string
		wantErr    bool
	}{
		{name: "repository root", value: "file.txt", want: "file.txt"},
		{name: "subdirectory", prefix: "sub/", value: "file.txt", want: "sub/file.txt"},
		{
			name:   "subdirectory parent remains inside",
			prefix: "sub/",
			value:  "../root.txt",
			want:   "root.txt",
		},
		{name: "missing target", value: "missing.txt", want: "missing.txt"},
		{name: "Unicode and space", value: "sub/space ü/file.txt", want: "sub/space ü/file.txt"},
		{name: "absolute inside", value: filepath.Join(root, "absolute.txt"), want: "absolute.txt"},
		{
			name:  "absolute missing target",
			value: filepath.Join(root, "missing-absolute.txt"),
			want:  "missing-absolute.txt",
		},
		{
			name:  "absolute through symlinked parent",
			value: filepath.Join(root, "linked-parent", "outside.txt"),
			want:  "linked-parent/outside.txt",
		},
		{
			name:       "ignore case existing components",
			value:      "casedir/tracked.TXT",
			ignoreCase: true,
			want:       "CaseDir/Tracked.txt",
		},
		{name: "preserve literal case", value: "casedir/tracked.TXT", want: "casedir/tracked.TXT"},
		{
			name:       "ignore case absolute root and path",
			value:      filepath.Join(wrongCaseRoot, "CASEDIR", "TRACKED.TXT"),
			ignoreCase: true,
			want:       "CaseDir/Tracked.txt",
		},
		{
			name:       "ignore case stops at symlink",
			value:      "LINKED-PARENT/OUTSIDE.TXT",
			ignoreCase: true,
			want:       "linked-parent/OUTSIDE.TXT",
		},
		{name: "relative outside", value: "../outside.txt", wantErr: true},
		{name: "absolute outside", value: outside, wantErr: true},
		{
			name:       "ignore case absolute outside",
			value:      strings.ToUpper(outside),
			ignoreCase: true,
			wantErr:    true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizePath(root, test.prefix, test.value, test.ignoreCase)
			if test.wantErr {
				if err == nil {
					t.Fatalf("normalizePath() = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("normalizePath() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestHasSymlinkParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "regular"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(root, "final")); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		path string
		want bool
	}{
		{path: "plain.txt", want: false},
		{path: "regular/file.txt", want: false},
		{path: "linked/file.txt", want: true},
		{path: "final", want: false},
	} {
		got, err := hasSymlinkParent(root, test.path)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("hasSymlinkParent(%q) = %t, want %t", test.path, got, test.want)
		}
	}
}
