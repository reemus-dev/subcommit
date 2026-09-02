package subcommit

import (
	"reflect"
	"testing"
)

func TestParseStatus(t *testing.T) {
	invalidPath := string([]byte{0xff, 0xfe, 'x'})
	for _, test := range []struct {
		name   string
		output []byte
		want   []statusEntry
	}{
		{
			name:   "modified",
			output: []byte(" M file.txt\x00"),
			want:   []statusEntry{{Code: " M", Path: "file.txt"}},
		},
		{
			name:   "untracked",
			output: []byte("?? new.txt\x00"),
			want:   []statusEntry{{Code: "??", Path: "new.txt"}},
		},
		{
			name:   "rename and copy ordering",
			output: []byte("R  destination.txt\x00original.txt\x00C  copy.txt\x00source.txt\x00"),
			want: []statusEntry{
				{Code: "R ", Path: "destination.txt", OriginalPath: "original.txt"},
				{Code: "C ", Path: "copy.txt", OriginalPath: "source.txt"},
			},
		},
		{
			name:   "worktree rename ordering",
			output: []byte(" R destination.txt\x00original.txt\x00"),
			want: []statusEntry{{
				Code: " R", Path: "destination.txt", OriginalPath: "original.txt",
			}},
		},
		{
			name:   "spaces",
			output: []byte(" M path with spaces \x00"),
			want:   []statusEntry{{Code: " M", Path: "path with spaces "}},
		},
		{
			name:   "newline and tab",
			output: []byte("?? line\n\tname\x00"),
			want:   []statusEntry{{Code: "??", Path: "line\n\tname"}},
		},
		{
			name:   "invalid UTF-8",
			output: append([]byte{'?', '?', ' '}, append([]byte(invalidPath), 0)...),
			want:   []statusEntry{{Code: "??", Path: invalidPath}},
		},
		{
			name:   "short records skipped",
			output: []byte("x\x00 M \x00?? kept\x00"),
			want:   []statusEntry{{Code: "??", Path: "kept"}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := parseStatus(test.output); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("parseStatus() = %#v, want %#v", got, test.want)
			}
		})
	}
}
