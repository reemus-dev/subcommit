package acceptance

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHooks(t *testing.T) {
	t.Run("pre-commit vetoes and sees isolated index", func(t *testing.T) {
		t.Run("veto", func(t *testing.T) {
			repo := newRepository(t)
			repo.seedBasic()
			repo.write("a.txt", []byte("modified\n"))
			repo.installHook("pre-commit")
			beforeHead, beforeIndex := repo.head(), repo.index()
			env := map[string]string{"SUBCOMMIT_HOOK_PRE_COMMIT": "reject"}

			result := repo.subcommit(
				nil, env, "a.txt", "-m", "veto", "--yes",
			)
			requireRefusal(t, result)
			if repo.head() != beforeHead || repo.index() != beforeIndex {
				t.Fatal("veto changed HEAD or real index")
			}
		})

		t.Run("isolated index", func(t *testing.T) {
			repo := newRepository(t)
			repo.seedBasic()
			repo.write("a.txt", []byte("modified\n"))
			repo.installHook("pre-commit")
			marker := filepath.Join(repo.dir, ".git", "cached-names")
			env := map[string]string{
				"SUBCOMMIT_HOOK_PRE_COMMIT": "record-cached",
				"SUBCOMMIT_HOOK_MARKER":     marker,
			}

			requireSuccess(t, repo.subcommit(nil, env, "a.txt", "-m", "inspect", "--yes"))
			content, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != "a.txt\n" {
				t.Fatalf("pre-commit cached paths = %q", content)
			}
		})
	})

	t.Run("noninteractive refusal happens before hooks and preview", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("modified\n"))
		repo.installHook("pre-commit")
		marker := filepath.Join(repo.dir, ".git", "preflight-marker")
		env := map[string]string{
			"SUBCOMMIT_HOOK_PRE_COMMIT": "touch",
			"SUBCOMMIT_HOOK_MARKER":     marker,
		}

		result := repo.subcommit(nil, env, "a.txt", "-m", "prompt")
		requireRefusal(t, result)
		if !strings.Contains(result.stderr, "pass --yes") ||
			strings.Contains(result.stderr, "Commit preview") {
			t.Fatalf("noninteractive preflight = %q", result.stderr)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("pre-commit ran before noninteractive refusal: %v", err)
		}
	})

	t.Run("hook inherits caller pathspec environment like native Git", func(t *testing.T) {
		pathspecKeys := []string{
			"GIT_LITERAL_PATHSPECS",
			"GIT_GLOB_PATHSPECS",
			"GIT_NOGLOB_PATHSPECS",
			"GIT_ICASE_PATHSPECS",
		}
		for _, supplied := range []bool{false, true} {
			name := "unset"
			if supplied {
				name = "supplied"
			}
			t.Run(name, func(t *testing.T) {
				if !supplied {
					for _, key := range pathspecKeys {
						if _, ok := os.LookupEnv(key); ok {
							t.Skipf("%s is set in the test environment", key)
						}
					}
				}
				nativeRepo := newRepository(t)
				helperRepo := newRepository(t)
				markers := make([]string, 0, 2)
				for _, repo := range []*repository{nativeRepo, helperRepo} {
					repo.seedBasic()
					repo.write("a.txt", []byte("modified\n"))
					repo.installHook("pre-commit")
					markers = append(markers, filepath.Join(repo.dir, ".git", "pathspec-env"))
				}
				environment := func(marker string) map[string]string {
					values := map[string]string{
						"SUBCOMMIT_HOOK_PRE_COMMIT": "record-pathspec-env",
						"SUBCOMMIT_HOOK_MARKER":     marker,
					}
					if supplied {
						for _, key := range pathspecKeys {
							values[key] = "0"
						}
					}
					return values
				}

				nativeResult := nativeRepo.gitResult(
					environment(markers[0]),
					"commit", "-q", "-m", "environment", "--", "a.txt",
				)
				if nativeResult.exitCode != 0 {
					t.Fatalf("native commit failed: %s", nativeResult.output())
				}
				requireSuccess(t, helperRepo.subcommit(
					nil, environment(markers[1]), "a.txt",
					"-m", "environment", "--yes",
				))
				nativeEnvironment, err := os.ReadFile(markers[0])
				if err != nil {
					t.Fatal(err)
				}
				helperEnvironment, err := os.ReadFile(markers[1])
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(helperEnvironment, nativeEnvironment) {
					t.Fatalf(
						"hook pathspec environment differs:\n"+
							"native:\n%shelper:\n%s",
						nativeEnvironment,
						helperEnvironment,
					)
				}
			})
		}
	})

	t.Run("complete-target hook may format and restage target", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("unformatted\n"))
		repo.installHook("pre-commit")
		env := map[string]string{
			"SUBCOMMIT_HOOK_PRE_COMMIT": "format-restage",
			"SUBCOMMIT_HOOK_TARGET":     "a.txt",
			"SUBCOMMIT_HOOK_CONTENT":    "formatted\n",
		}

		result := repo.subcommit(nil, env, "a.txt", "-m", "format", "--yes")
		requireSuccess(t, result)
		if !strings.Contains(result.stderr, "Hook changes") ||
			!strings.Contains(result.stderr, "a.txt") ||
			strings.Contains(result.stderr, "Canceling will not undo") {
			t.Fatalf("hook-change disclosure = %q", result.stderr)
		}
		if got := repo.headFile("a.txt"); !bytes.Equal(got, []byte("formatted\n")) {
			t.Fatalf("formatted commit = %q", got)
		}
		if got := repo.read("a.txt"); !bytes.Equal(got, []byte("formatted\n")) {
			t.Fatalf("formatted worktree = %q", got)
		}
	})

	t.Run("complete-target hook cannot remove a requested change", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("selected a\n"))
		repo.write("b.txt", []byte("selected b\n"))
		repo.installHook("pre-commit")
		env := map[string]string{
			"SUBCOMMIT_HOOK_PRE_COMMIT": "format-restage",
			"SUBCOMMIT_HOOK_TARGET":     "a.txt",
			"SUBCOMMIT_HOOK_CONTENT":    "a1\n",
		}
		beforeHead, beforeIndex := repo.head(), repo.index()

		result := repo.subcommit(
			nil, env, "a.txt", "b.txt",
			"-m", "contract selection", "--yes",
		)
		requireRefusal(t, result)
		if !strings.Contains(result.stderr, "pre-commit removed all selected changes from a target") ||
			!strings.Contains(result.stderr, "a.txt") {
			t.Fatalf("hook contraction refusal = %q", result.stderr)
		}
		if repo.head() != beforeHead || repo.index() != beforeIndex {
			t.Fatal("hook contraction changed HEAD or real index")
		}
	})

	t.Run("complete-target hook cannot break an inferred move", func(t *testing.T) {
		repo := newRepository(t)
		repo.write("old.txt", []byte("moved\n"))
		repo.git("add", "old.txt")
		repo.git("commit", "-q", "-m", "base")
		repo.git("mv", "old.txt", "new.txt")
		repo.installHook("pre-commit")
		env := map[string]string{
			"SUBCOMMIT_HOOK_PRE_COMMIT": "format-restage",
			"SUBCOMMIT_HOOK_TARGET":     "old.txt",
			"SUBCOMMIT_HOOK_CONTENT":    "recreated\n",
		}
		beforeHead, beforeIndex := repo.head(), repo.index()

		result := repo.subcommit(nil, env, "new.txt", "-m", "move", "--yes")
		requireRefusal(t, result)
		if !strings.Contains(result.stderr, "pre-commit no longer preserves the inferred move") ||
			!strings.Contains(result.stderr, "old.txt -> new.txt") {
			t.Fatalf("inferred move refusal = %q", result.stderr)
		}
		if repo.head() != beforeHead || repo.index() != beforeIndex {
			t.Fatal("inferred move refusal changed HEAD or real index")
		}
	})

	t.Run("complete-target hook cannot create an unchanged selection", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("b.txt", []byte("selected worktree\n"))
		repo.installHook("pre-commit")
		env := map[string]string{
			"SUBCOMMIT_HOOK_PRE_COMMIT": "format-restage",
			"SUBCOMMIT_HOOK_TARGET":     "a.txt",
			"SUBCOMMIT_HOOK_CONTENT":    "hook formatted\n",
		}
		before := repo.fingerprint()

		result := repo.subcommit(
			nil, env, "a.txt", "b.txt",
			"-m", "format selected", "--yes",
		)
		requireRefusal(t, result)
		if !strings.Contains(result.stderr, "a.txt  no changes relative to the current commit") {
			t.Fatalf("unchanged selection refusal = %q", result.stderr)
		}
		if got := repo.fingerprint(); got != before {
			t.Fatal("unchanged selection refusal ran hooks or changed repository state")
		}
	})

	t.Run("complete-target hook chmod ignored by core.fileMode false", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows worktrees do not expose Unix executable mode")
		}
		repo := newRepository(t)
		repo.seedBasic()
		repo.git("config", "core.fileMode", "false")
		repo.write("a.txt", []byte("modified\n"))
		repo.installHook("pre-commit")
		env := map[string]string{
			"SUBCOMMIT_HOOK_PRE_COMMIT": "chmod-executable",
			"SUBCOMMIT_HOOK_TARGET":     "a.txt",
		}

		requireSuccess(t, repo.subcommit(nil, env, "a.txt", "-m", "ignore hook chmod", "--yes"))
		if entry := repo.git("ls-tree", "HEAD", "--", "a.txt"); !strings.HasPrefix(entry, "100644 ") {
			t.Fatalf("ignored hook chmod changed HEAD mode: %s", entry)
		}
		info, err := os.Stat(filepath.Join(repo.dir, "a.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Fatal("hook chmod did not remain in worktree")
		}
	})

	t.Run("scope expansion aborts", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("modified\n"))
		repo.write("b.txt", []byte("dirty\n"))
		repo.installHook("pre-commit")
		beforeHead, beforeIndex := repo.head(), repo.index()
		env := map[string]string{
			"SUBCOMMIT_HOOK_PRE_COMMIT": "append-stage",
			"SUBCOMMIT_HOOK_TARGET":     "b.txt",
			"SUBCOMMIT_HOOK_CONTENT":    "expanded\n",
		}

		result := repo.subcommit(nil, env, "a.txt", "-m", "reject expansion", "--yes")
		requireRefusal(t, result)
		if repo.head() != beforeHead || repo.index() != beforeIndex {
			t.Fatal("scope expansion changed HEAD or real index")
		}
		if !strings.Contains(result.stderr, "unselected path") {
			t.Fatalf("refusal lacks scope evidence:\n%s", result.stderr)
		}
	})

	t.Run("complete-target hook may restage beside range target", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("f.txt", []byte("one\ntwo\nthree\nfour\nfive\n"))
		repo.git("add", "f.txt")
		repo.git("commit", "-q", "-m", "seed range target")
		repo.write("a.txt", []byte("unformatted\n"))
		repo.write("f.txt", []byte("one\nTWO\nthree\nFOUR\nfive\n"))
		repo.installHook("pre-commit")
		env := map[string]string{
			"SUBCOMMIT_HOOK_PRE_COMMIT": "format-restage",
			"SUBCOMMIT_HOOK_TARGET":     "a.txt",
			"SUBCOMMIT_HOOK_CONTENT":    "formatted\n",
		}

		requireSuccess(t, repo.subcommit(
			nil, env, "a.txt", "f.txt:2", "-m", "mixed targets", "--yes",
		))
		if got := repo.headFile("a.txt"); !bytes.Equal(got, []byte("formatted\n")) {
			t.Fatalf("formatted complete target = %q", got)
		}
		if got := repo.headFile("f.txt"); !bytes.Equal(got, []byte("one\nTWO\nthree\nfour\nfive\n")) {
			t.Fatalf("committed range target = %q", got)
		}
	})

	t.Run("range-target mutation is refused beside complete target", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("f.txt", []byte("one\ntwo\nthree\nfour\nfive\n"))
		repo.git("add", "f.txt")
		repo.git("commit", "-q", "-m", "seed range target")
		repo.write("a.txt", []byte("complete target\n"))
		modified := []byte("one\nTWO\nthree\nFOUR\nfive\n")
		repo.write("f.txt", modified)
		repo.installHook("pre-commit")
		beforeHead, beforeIndex := repo.head(), repo.index()
		env := map[string]string{
			"SUBCOMMIT_HOOK_PRE_COMMIT": "format-restage",
			"SUBCOMMIT_HOOK_TARGET":     "f.txt",
			"SUBCOMMIT_HOOK_CONTENT":    string(modified),
		}

		result := repo.subcommit(
			nil, env, "a.txt", "f.txt:2", "-m", "reject range mutation", "--yes",
		)
		requireRefusal(t, result)
		if repo.head() != beforeHead || repo.index() != beforeIndex {
			t.Fatal("range-target hook changed HEAD or real index")
		}
		if !strings.Contains(result.stderr, "range-selected file") ||
			!strings.Contains(result.stderr, "select the complete file without :ranges") {
			t.Fatalf("refusal lacks range-target policy evidence:\n%s", result.stderr)
		}
	})

	t.Run("message and lifecycle hooks", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("modified\n"))
		for _, hook := range []string{"prepare-commit-msg", "commit-msg", "post-commit"} {
			repo.installHook(hook)
		}
		marker := filepath.Join(repo.dir, ".git", "post-marker")
		env := map[string]string{
			"SUBCOMMIT_HOOK_PREPARE_COMMIT_MSG": "append-message",
			"SUBCOMMIT_HOOK_COMMIT_MSG":         "rewrite-message",
			"SUBCOMMIT_HOOK_POST_COMMIT":        "touch",
			"SUBCOMMIT_HOOK_CONTENT":            "[ACCEPT] ",
			"SUBCOMMIT_HOOK_MARKER":             marker,
		}

		requireSuccess(t, repo.subcommit(nil, env, "a.txt", "-m", "message", "--yes"))
		message := repo.git("log", "-1", "--pretty=%B")
		if !strings.HasPrefix(message, "[ACCEPT] message") {
			t.Fatalf("hook-rewritten message = %q", message)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("post-commit marker: %v", err)
		}
	})

	t.Run("post-commit output stays on stderr and failure remains successful", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("modified\n"))
		repo.installHook("post-commit")
		env := map[string]string{
			"SUBCOMMIT_HOOK_POST_COMMIT": "print-reject",
			"SUBCOMMIT_HOOK_CONTENT":     "post-commit stdout",
		}

		result := repo.subcommit(nil, env, "a.txt", "-m", "message", "--yes")
		requireSuccess(t, result)
		if !strings.Contains(result.stderr, "post-commit stdout") ||
			!strings.Contains(result.stderr, "post-commit rejected") ||
			!strings.Contains(result.stderr, "warning: post-commit hook failed") {
			t.Fatalf("post-commit diagnostic = %q", result.stderr)
		}
	})

	t.Run("no-verify skips veto hooks but keeps prepare and post", func(t *testing.T) {
		repo := newRepository(t)
		repo.seedBasic()
		repo.write("a.txt", []byte("modified\n"))
		for _, hook := range []string{"pre-commit", "prepare-commit-msg", "commit-msg", "post-commit"} {
			repo.installHook(hook)
		}
		marker := filepath.Join(repo.dir, ".git", "post-marker")
		env := map[string]string{
			"SUBCOMMIT_HOOK_PRE_COMMIT":         "reject",
			"SUBCOMMIT_HOOK_PREPARE_COMMIT_MSG": "append-message",
			"SUBCOMMIT_HOOK_COMMIT_MSG":         "reject",
			"SUBCOMMIT_HOOK_POST_COMMIT":        "touch",
			"SUBCOMMIT_HOOK_CONTENT":            "\nPrepared: yes\n",
			"SUBCOMMIT_HOOK_MARKER":             marker,
		}

		requireSuccess(t, repo.subcommit(
			nil, env, "a.txt", "-m", "message", "--yes", "--no-verify",
		))
		if message := repo.git("log", "-1", "--pretty=%B"); !strings.Contains(message, "Prepared: yes") {
			t.Fatalf("prepare-commit-msg did not run: %q", message)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("post-commit marker: %v", err)
		}
	})
}
