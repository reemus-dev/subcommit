package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTargetUsesFinalValidRangeSuffix(t *testing.T) {
	target, err := parseTarget(`C:\repo\file.go:42-48,60`)
	if err != nil {
		t.Fatal(err)
	}
	if target.Path != `C:\repo\file.go` {
		t.Fatalf("path = %q", target.Path)
	}
	if len(target.Ranges) != 2 ||
		target.Ranges[0].Start != 42 ||
		target.Ranges[0].End != 48 ||
		target.Ranges[1].Start != 60 ||
		target.Ranges[1].End != 60 {
		t.Fatalf("ranges = %#v", target.Ranges)
	}
}

func TestParseTargetWithoutRangeSelectsCompleteFile(t *testing.T) {
	target, err := parseTarget("file.go")
	if err != nil {
		t.Fatal(err)
	}
	if target.Path != "file.go" || len(target.Ranges) != 0 {
		t.Fatalf("target = %#v", target)
	}
}

func TestParseTargetRejectsInvalidRanges(t *testing.T) {
	for _, value := range []string{
		":42",
		"file.go:2-",
		"file.go:999999999999999999999999999999",
	} {
		if _, err := parseTarget(value); err == nil {
			t.Errorf("parseTarget(%q) succeeded", value)
		}
	}
}

func TestGeneratedCompletionIncludesExecutableAndGitBridges(t *testing.T) {
	tests := map[string][]string{
		"bash": {
			"complete -o default -F __start_subcommit git-subcommit",
			"_git_subcommit()",
			"local COMP_WORDS=(git-subcommit",
		},
		"zsh": {
			"compdef _subcommit git-subcommit",
			"_git_subcommit()",
		},
		"fish": {
			"complete -c git-subcommit -w subcommit",
		},
		"powershell": {
			"Register-ArgumentCompleter -CommandName 'git-subcommit'",
		},
	}
	for shell, expected := range tests {
		t.Run(shell, func(t *testing.T) {
			var output bytes.Buffer
			if err := GenerateCompletion(NewRootCommand(), shell, &output); err != nil {
				t.Fatal(err)
			}
			for _, fragment := range expected {
				if !strings.Contains(output.String(), fragment) {
					t.Errorf("generated completion lacks %q", fragment)
				}
			}
		})
	}
}

func TestGeneratedZshCompletionDispatchesGitSubcommand(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	var output bytes.Buffer
	if err := GenerateCompletion(NewRootCommand(), "zsh", &output); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "_subcommit")
	if err := os.WriteFile(path, output.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `autoload -Uz compinit; compinit -d "$COMPDUMP"; ` +
		`source "$COMPLETION_PATH"; ` +
		`_subcommit() { print dispatch-ok }; _git_subcommit`
	command := exec.Command(zsh, "-f", "-c", script)
	command.Env = append(
		os.Environ(),
		"COMPLETION_PATH="+path,
		"COMPDUMP="+filepath.Join(t.TempDir(), "zcompdump"),
	)
	result, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Zsh Git dispatch failed: %v\n%s", err, result)
	}
	if strings.TrimSpace(string(result)) != "dispatch-ok" {
		t.Fatalf("Zsh Git dispatch output = %q", result)
	}
}

func TestGeneratedBashAndZshCompletionHaveValidSyntax(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			binary, err := exec.LookPath(shell)
			if err != nil {
				t.Skipf("%s is not installed", shell)
			}
			var output bytes.Buffer
			if err := GenerateCompletion(NewRootCommand(), shell, &output); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "completion")
			if err := os.WriteFile(path, output.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			if result, err := exec.Command(binary, "-n", path).CombinedOutput(); err != nil {
				t.Fatalf("invalid %s completion: %v\n%s", shell, err, result)
			}
		})
	}
}

func TestCommitTargetsUseShellFileCompletion(t *testing.T) {
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"__complete", "example"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != ":0" {
		t.Fatalf("completion directive = %q, want shell default file completion", output.String())
	}
}

func TestZeroArgumentsShowHelp(t *testing.T) {
	stdout, stderr, err := executeCommand(t)
	if err != nil {
		t.Fatalf("zero-argument invocation failed: %v", err)
	}

	helpStdout, helpStderr, helpErr := executeCommand(t, "--help")
	if helpErr != nil {
		t.Fatalf("help invocation failed: %v", helpErr)
	}
	if stdout != helpStdout || stderr != helpStderr {
		t.Fatalf(
			"zero-argument output differs from --help:\nstdout: %q\nstderr: %q",
			stdout,
			stderr,
		)
	}
}

func TestInvocationErrorsShowConciseUsageButRuntimeErrorsDoNot(t *testing.T) {
	t.Run("invocation error", func(t *testing.T) {
		_, stderr, err := executeCommand(t, "a.txt")
		if err == nil {
			t.Fatal("missing message source succeeded")
		}
		if !strings.Contains(stderr, "usage: subcommit [<path|path:ranges>") ||
			strings.Contains(stderr, "Flags:") {
			t.Fatalf("invocation diagnostic = %q", stderr)
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		_, stderr, err := executeCommand(
			t, "missing.txt", "-m", "message", "--yes",
		)
		if err == nil {
			t.Fatal("non-repository invocation succeeded")
		}
		if strings.Contains(stderr, "usage:") || strings.Contains(stderr, "Flags:") {
			t.Fatalf("runtime diagnostic dumped usage: %q", stderr)
		}
	})
}

func TestFlagErrorsShowConciseUsage(t *testing.T) {
	_, stderr, err := executeCommand(t, "a.txt", "--unknown")
	if err == nil {
		t.Fatal("unknown flag succeeded")
	}
	if !strings.Contains(stderr, "error: unknown flag: --unknown") ||
		!strings.Contains(stderr, "usage: subcommit [<path|path:ranges>") ||
		strings.Contains(stderr, "Flags:") {
		t.Fatalf("flag diagnostic = %q", stderr)
	}
}

func TestCommitRequiresExactlyOneMessageSource(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		_, stderr, err := executeCommand(t, "a.txt", "--yes")
		if err == nil {
			t.Fatal("missing message source succeeded")
		}
		if !strings.Contains(stderr, "one of --message or --file is required") {
			t.Fatalf("missing-source diagnostic = %q", stderr)
		}
	})

	t.Run("both", func(t *testing.T) {
		_, stderr, err := executeCommand(
			t, "a.txt", "--message=message", "--file=-", "--yes",
		)
		if err == nil {
			t.Fatal("multiple message sources succeeded")
		}
		if !strings.Contains(stderr, "--message and --file cannot be used together") {
			t.Fatalf("multiple-source diagnostic = %q", stderr)
		}
	})
}

func TestEmptyMessageIsDistinguishedFromMissingSource(t *testing.T) {
	_, stderr, err := executeCommand(t, "a.txt", "--message=")
	if err == nil {
		t.Fatal("empty message succeeded")
	}
	if !strings.Contains(stderr, "commit message cannot be empty") ||
		strings.Contains(stderr, "one of --message or --file is required") {
		t.Fatalf("empty-message diagnostic = %q", stderr)
	}
}

func TestNoninteractivePreflightDoesNotReadMessageStdin(t *testing.T) {
	stdin := &readTrackingInput{}
	command := NewRootCommand()
	command.SetIn(stdin)
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"a.txt", "-F", "-"})

	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "cannot prompt for confirmation") {
		t.Fatalf("error = %v", err)
	}
	if stdin.read {
		t.Fatal("noninteractive preflight consumed stdin")
	}
}

type readTrackingInput struct {
	read bool
}

func (input *readTrackingInput) Read([]byte) (int, error) {
	input.read = true
	return 0, errors.New("unexpected stdin read")
}

func TestCompletionFlag(t *testing.T) {
	t.Run("generates requested shell", func(t *testing.T) {
		stdout, stderr, err := executeCommand(t, "--completion", "bash")
		if err != nil {
			t.Fatalf("completion failed: %v\n%s", err, stderr)
		}
		if !strings.Contains(stdout, "__start_subcommit") || stderr != "" {
			t.Fatalf("completion output = %q, %q", stdout, stderr)
		}
	})

	t.Run("refuses commit arguments", func(t *testing.T) {
		_, stderr, err := executeCommand(t, "--completion", "bash", "a.txt")
		if err == nil {
			t.Fatal("completion with target succeeded")
		}
		if !strings.Contains(stderr, "--completion cannot be combined") {
			t.Fatalf("completion diagnostic = %q", stderr)
		}
	})
}

func TestExplicitCompletePathBypassesRangeParsing(t *testing.T) {
	_, stderr, err := executeCommand(
		t, "--complete", "report:42", "-m", "message", "--yes",
	)
	if err == nil || !strings.Contains(stderr, "selected path does not exist") ||
		strings.Contains(stderr, "invalid ranges") {
		t.Fatalf("explicit complete diagnostic = %q", stderr)
	}
}

func TestInvalidColorModeUsesPlainDiagnostic(t *testing.T) {
	_, stderr, err := executeCommand(
		t, "missing.txt", "-m", "message", "--yes", "--color=rainbow",
	)
	if err == nil {
		t.Fatal("invalid color mode succeeded")
	}
	if !strings.Contains(stderr, `error: invalid color mode "rainbow"`) ||
		strings.Contains(stderr, "\x1b[") {
		t.Fatalf("invalid color diagnostic = %q", stderr)
	}
}

func TestQuietAndVerboseAreMutuallyExclusive(t *testing.T) {
	_, stderr, err := executeCommand(
		t, "missing.txt", "-m", "message", "--yes", "--quiet", "--verbose",
	)
	if err == nil {
		t.Fatal("quiet and verbose succeeded together")
	}
	if !strings.Contains(stderr, "--quiet and --verbose cannot be used together") {
		t.Fatalf("flag diagnostic = %q", stderr)
	}
}

func executeCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	command := NewRootCommand()
	var stdout, stderr bytes.Buffer
	command.SetIn(bytes.NewReader(nil))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs(args)
	err := command.Execute()
	if err != nil {
		renderError(command, err)
	}
	return stdout.String(), stderr.String(), err
}
