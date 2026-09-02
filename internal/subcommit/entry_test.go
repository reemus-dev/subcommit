package subcommit

import "testing"

func TestParseGitEntry(t *testing.T) {
	for _, test := range []struct {
		name     string
		record   string
		oidField int
		want     gitEntry
		valid    bool
	}{
		{
			name:     "index entry",
			record:   "100644 abc123 0",
			oidField: 1,
			want:     gitEntry{Mode: "100644", OID: "abc123"},
			valid:    true,
		},
		{
			name:     "tree entry",
			record:   "100755 blob def456",
			oidField: 2,
			want:     gitEntry{Mode: "100755", OID: "def456"},
			valid:    true,
		},
		{name: "missing OID", record: "100644", oidField: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, valid := parseGitEntry([]byte(test.record), test.oidField)
			if got != test.want || valid != test.valid {
				t.Fatalf("parseGitEntry() = (%+v, %t), want (%+v, %t)", got, valid, test.want, test.valid)
			}
		})
	}
}
