package acceptance

import "testing"

func TestNativeGitComparison(t *testing.T) {
	tests := []struct {
		name          string
		mutate        func(*repository)
		nativeArgs    []string
		subcommitArgs []string
		env           map[string]string
		identical     bool
		nativeSuccess bool
		helperSuccess bool
	}{
		{
			name: "plain unstaged subset",
			mutate: func(repo *repository) {
				repo.write("a.txt", []byte("a-modified\n"))
				repo.write("b.txt", []byte("b-modified\n"))
				repo.write("c.txt", []byte("c-dirty\n"))
			},
			nativeArgs:    []string{"commit", "-q", "-m", "subset", "--", "a.txt", "b.txt"},
			subcommitArgs: []string{"a.txt", "b.txt", "-m", "subset", "--yes"},
			identical:     true,
			nativeSuccess: true,
			helperSuccess: true,
		},
		{
			name: "unrelated prestaged file",
			mutate: func(repo *repository) {
				repo.write("a.txt", []byte("a-modified\n"))
				repo.write("b.txt", []byte("b-modified\n"))
				repo.write("c.txt", []byte("c-staged\n"))
				repo.git("add", "c.txt")
			},
			nativeArgs:    []string{"commit", "-q", "-m", "subset", "--", "a.txt", "b.txt"},
			subcommitArgs: []string{"a.txt", "b.txt", "-m", "subset", "--yes"},
			identical:     true,
			nativeSuccess: true,
			helperSuccess: true,
		},
		{
			name: "partly staged target",
			mutate: func(repo *repository) {
				repo.write("a.txt", []byte("staged\n"))
				repo.git("add", "a.txt")
				repo.write("a.txt", []byte("staged\nunstaged\n"))
			},
			nativeArgs:    []string{"commit", "-q", "-m", "target", "--", "a.txt"},
			subcommitArgs: []string{"a.txt", "-m", "target", "--yes"},
			identical:     true,
			nativeSuccess: true,
			helperSuccess: true,
		},
		{
			name: "tracked deletion",
			mutate: func(repo *repository) {
				repo.remove("a.txt")
				repo.write("c.txt", []byte("c-dirty\n"))
			},
			nativeArgs:    []string{"commit", "-q", "-m", "delete", "--", "a.txt"},
			subcommitArgs: []string{"a.txt", "-m", "delete", "--yes"},
			identical:     true,
			nativeSuccess: true,
			helperSuccess: true,
		},
		{
			name: "untracked target",
			mutate: func(repo *repository) {
				repo.write("new.txt", []byte("new\n"))
				repo.write("c.txt", []byte("c-dirty\n"))
			},
			nativeArgs:    []string{"commit", "-q", "-m", "new", "--", "new.txt"},
			subcommitArgs: []string{"new.txt", "-m", "new", "--yes"},
			identical:     false,
			nativeSuccess: false,
			helperSuccess: true,
		},
		{
			name: "hook attempts scope leak",
			mutate: func(repo *repository) {
				repo.write("a.txt", []byte("a-modified\n"))
				repo.write("c.txt", []byte("c-dirty\n"))
				repo.installHook("pre-commit")
			},
			nativeArgs:    []string{"commit", "-q", "-m", "target", "--", "a.txt"},
			subcommitArgs: []string{"a.txt", "-m", "target", "--yes"},
			env: map[string]string{
				"SUBCOMMIT_HOOK_PRE_COMMIT": "append-stage",
				"SUBCOMMIT_HOOK_TARGET":     "c.txt",
				"SUBCOMMIT_HOOK_CONTENT":    "hook-injected\n",
			},
			identical:     false,
			nativeSuccess: true,
			helperSuccess: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			nativeRepo := newComparisonRepository(t)
			helperRepo := newComparisonRepository(t)
			test.mutate(nativeRepo)
			test.mutate(helperRepo)

			nativeResult := nativeRepo.gitResult(test.env, test.nativeArgs...)
			helperResult := helperRepo.subcommit(nil, test.env, test.subcommitArgs...)
			if got := nativeResult.exitCode == 0; got != test.nativeSuccess {
				t.Fatalf("native success = %t, want %t:\n%s", got, test.nativeSuccess, nativeResult.output())
			}
			if got := helperResult.exitCode == 0; got != test.helperSuccess {
				t.Fatalf("helper success = %t, want %t:\n%s", got, test.helperSuccess, helperResult.output())
			}

			equal := nativeRepo.fingerprint() == helperRepo.fingerprint()
			if equal != test.identical {
				t.Fatalf(
					"end-state identical = %t, want %t\n"+
						"native status:\n%s\nhelper status:\n%s",
					equal,
					test.identical,
					nativeRepo.status(),
					helperRepo.status(),
				)
			}
		})
	}
}

func newComparisonRepository(t *testing.T) *repository {
	t.Helper()
	repo := newRepository(t)
	repo.write("a.txt", []byte("a1\n"))
	repo.write("b.txt", []byte("b1\n"))
	repo.write("c.txt", []byte("c1\n"))
	repo.git("add", "-A")
	repo.git("commit", "-q", "-m", "base")
	return repo
}
