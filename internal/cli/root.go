// Package cli defines the Cobra command tree and process-facing execution boundary.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/reemus-dev/subcommit/internal/console"
	"github.com/reemus-dev/subcommit/internal/subcommit"
	"github.com/spf13/cobra"
)

// Version is the build version reported by the root command.
// Release builds replace the development value through linker flags.
var Version = "dev"

// Execute runs the root command and renders any returned error.
func Execute(ctx context.Context) error {
	root := NewRootCommand()
	if err := root.ExecuteContext(ctx); err != nil {
		renderError(root, err)
		return err
	}
	return nil
}

// NewRootCommand builds a fresh command tree for execution and completion generation.
func NewRootCommand() *cobra.Command {
	var message, messageFile, completion string
	var completePaths []string
	var yes, noVerify, quiet, verbose bool

	root := &cobra.Command{
		Use: "subcommit [<path|path:ranges>...] [--complete <path>]... " +
			"(-m <message> | -F <file>)",
		Short: "Safely commit selected files and changed regions",
		Long: `Create a commit from explicit worktree changes without committing or losing
unrelated repository state.

Use native Git when:
  - the staged changes already exactly match the next commit
  - you only need complete changes from tracked paths

Use subcommit when:
  - selecting changed regions, untracked files, or mixed target types
  - unrelated staged, unstaged, or untracked changes must remain untouched
  - every requested target and hook change must stay within an explicit scope

Targets:
  path              Select the complete current file
  path:12           Select changed regions overlapping line 12
  path:12,20-24     Select regions overlapping multiple ranges
  --complete path   Select a literal complete path, including names such as report:42

Range numbers refer to lines in the current file. An overlapping contiguous
change region is committed as a unit.

Guarantees:
  - Selection is derived from HEAD to the current worktree
  - Every requested target must contribute a committable change
  - Unrelated index and worktree state is preserved
  - Selected files are not rewritten by subcommit
  - Unique exact-content moves include both endpoints
  - Hooks run against an isolated candidate and cannot silently expand scope

Hooks may modify selected worktree files. Accepted hook changes are reported and
are not undone if confirmation is canceled.`,
		Example: `  subcommit internal/app.go internal/utils.go -m "update validation"
  subcommit internal/app.go:42-48 README.md:12 -m "document validation"
  subcommit --complete report:42 -m "update report"
  subcommit old.go new.go -m "move and update parser"
  printf 'subject\n\nbody\n' | subcommit internal/app.go:42-48 -F - --yes`,
		Version:       Version,
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveDefault
		},
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 0 && command.Flags().NFlag() == 0 {
				return command.Help()
			}

			messageSet := command.Flags().Changed("message")
			fileSet := command.Flags().Changed("file")
			if command.Flags().Changed("completion") {
				if completion == "" {
					return completionInvocationError("completion shell cannot be empty")
				}
				if len(args) > 0 || len(completePaths) > 0 || messageSet || fileSet ||
					yes || noVerify || quiet || verbose {
					return completionInvocationError(
						"--completion cannot be combined with commit targets or flags",
					)
				}
				return GenerateCompletion(command.Root(), completion, command.OutOrStdout())
			}
			if len(args) == 0 && len(completePaths) == 0 {
				return invocationError("at least one path is required")
			}
			if messageSet && fileSet {
				return invocationError("--message and --file cannot be used together")
			}
			if !messageSet && !fileSet {
				return invocationError("one of --message or --file is required")
			}
			if messageSet && strings.TrimSpace(message) == "" {
				return invocationError("commit message cannot be empty")
			}

			targets, err := parseTargets(args)
			if err != nil {
				return err
			}
			for _, path := range completePaths {
				targets = append(targets, subcommit.Target{Path: path})
			}

			color, _ := command.Root().PersistentFlags().GetString("color")
			output, err := console.New(
				command.InOrStdin(), command.OutOrStdout(), command.ErrOrStderr(),
				console.Options{
					Color:   console.ColorMode(color),
					Quiet:   quiet,
					Verbose: verbose,
				},
			)
			if err != nil {
				return err
			}
			if !yes && !output.CanPrompt() {
				return &console.Diagnostic{
					Summary: "cannot prompt for confirmation in a non-interactive session",
					Hint:    "pass --yes to confirm the commit noninteractively",
				}
			}
			if fileSet {
				message, err = readCommitMessage(messageFile, command.InOrStdin())
				if err != nil {
					return err
				}
				if strings.TrimSpace(message) == "" {
					return invocationError("commit message cannot be empty")
				}
			}

			return subcommit.Run(
				command.Context(),
				subcommit.Request{
					Message:  message,
					Yes:      yes,
					NoVerify: noVerify,
					Targets:  targets,
				},
				output,
			)
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetFlagErrorFunc(func(command *cobra.Command, err error) error {
		return &console.Diagnostic{
			Summary: err.Error(),
			Usage:   command.UseLine(),
			Hint:    fmt.Sprintf("run `%s --help` for available options", command.CommandPath()),
		}
	})
	root.PersistentFlags().String(
		"color", string(console.ColorAuto), "color output: auto, always, or never",
	)
	root.Flags().StringVarP(&message, "message", "m", "", "commit message")
	root.Flags().StringVarP(
		&messageFile, "file", "F", "", "read commit message from file, use - for stdin",
	)
	root.Flags().StringArrayVar(
		&completePaths, "complete", nil,
		"select a complete literal path, repeat for multiple paths",
	)
	root.Flags().StringVar(
		&completion, "completion", "",
		"generate completion for bash, zsh, fish, or powershell",
	)
	root.Flags().BoolVarP(&yes, "yes", "y", false, "publish without interactive confirmation")
	root.Flags().BoolVarP(
		&noVerify, "no-verify", "n", false,
		"skip pre-commit and commit-msg hooks",
	)
	root.Flags().BoolVarP(&quiet, "quiet", "q", false, "suppress preview output")
	root.Flags().BoolVarP(&verbose, "verbose", "v", false, "show all preview details")
	_ = root.RegisterFlagCompletionFunc(
		"complete",
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveDefault
		},
	)
	return root
}

func renderError(command *cobra.Command, commandErr error) {
	color, _ := command.Root().PersistentFlags().GetString("color")
	output, err := console.New(
		command.InOrStdin(), command.OutOrStdout(), command.ErrOrStderr(),
		console.Options{Color: console.ColorMode(color)},
	)
	if err != nil {
		output, _ = console.New(
			command.InOrStdin(), command.OutOrStdout(), command.ErrOrStderr(),
			console.Options{Color: console.ColorNever},
		)
	}
	output.RenderError(commandErr)
}

func readCommitMessage(path string, stdin io.Reader) (string, error) {
	var content []byte
	var err error
	source := fmt.Sprintf("file %q", path)
	if path == "-" {
		content, err = io.ReadAll(stdin)
		source = "standard input"
	} else {
		content, err = os.ReadFile(path)
	}
	if err != nil {
		return "", &console.Diagnostic{
			Summary: "cannot read commit message from " + source,
			Sections: []console.Section{{
				Title: "Details",
				Lines: []string{err.Error()},
			}},
			Cause: err,
		}
	}
	return string(content), nil
}

func parseTargets(args []string) ([]subcommit.Target, error) {
	targets := make([]subcommit.Target, 0, len(args))
	for _, value := range args {
		target, err := parseTarget(value)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func invocationError(summary string) error {
	return &console.Diagnostic{
		Summary: summary,
		Usage:   commandUsage(),
		Hint:    "run `subcommit --help` for examples",
	}
}

func commandUsage() string {
	return "subcommit [<path|path:ranges>...] [--complete <path>] " +
		"(-m <message> | -F <file>)"
}

func completionInvocationError(summary string) error {
	return &console.Diagnostic{
		Summary: summary,
		Usage:   "subcommit --completion <bash|zsh|fish|powershell>",
		Hint:    "use --completion by itself with one supported shell",
	}
}

var rangeTargetSuffix = regexp.MustCompile(`:([0-9]+(?:-[0-9]+)?(?:,[0-9]+(?:-[0-9]+)?)*)$`)

func parseTarget(value string) (subcommit.Target, error) {
	match := rangeTargetSuffix.FindStringSubmatchIndex(value)
	if match == nil {
		if colon := trailingTargetColon(value); colon >= 0 {
			suffix := value[colon+1:]
			if suffix == "" || suffix[0] >= '0' && suffix[0] <= '9' {
				return subcommit.Target{}, invocationError(fmt.Sprintf(
					"invalid ranges %q in %q (use --complete for the literal path)",
					suffix, value,
				))
			}
		}
		return subcommit.Target{Path: value}, nil
	}
	if match[0] == 0 {
		return subcommit.Target{}, invocationError(
			fmt.Sprintf("expected a path before ranges in %q", value),
		)
	}

	rawRanges := value[match[2]:match[3]]
	parts := strings.Split(rawRanges, ",")
	ranges := make([]subcommit.Range, 0, len(parts))
	for _, part := range parts {
		bounds := strings.SplitN(part, "-", 2)
		start, err := strconv.Atoi(bounds[0])
		if err != nil {
			return subcommit.Target{}, invalidRangeError(part, value)
		}
		end := start
		if len(bounds) == 2 {
			end, err = strconv.Atoi(bounds[1])
			if err != nil {
				return subcommit.Target{}, invalidRangeError(part, value)
			}
		}
		if start < 1 || end < start {
			return subcommit.Target{}, invalidRangeError(part, value)
		}
		ranges = append(ranges, subcommit.Range{Start: start, End: end})
	}
	return subcommit.Target{Path: value[:match[0]], Ranges: ranges}, nil
}

func invalidRangeError(value, target string) error {
	return invocationError(fmt.Sprintf("invalid range %q in %q", value, target))
}

func trailingTargetColon(value string) int {
	colon := strings.LastIndexByte(value, ':')
	if colon < 0 {
		return -1
	}
	separator := strings.LastIndexAny(value, `/\\`)
	if colon <= separator {
		return -1
	}
	return colon
}

// GenerateCompletion writes Cobra completion followed by Git dispatch bridges.
func GenerateCompletion(root *cobra.Command, shell string, output io.Writer) error {
	var err error
	switch shell {
	case "bash":
		err = root.GenBashCompletion(output)
	case "zsh":
		err = root.GenZshCompletion(output)
	case "fish":
		err = root.GenFishCompletion(output, true)
	case "powershell":
		err = root.GenPowerShellCompletion(output)
	default:
		return &console.Diagnostic{
			Summary: fmt.Sprintf("unsupported completion shell %q", shell),
			Hint:    "use bash, zsh, fish, or powershell",
		}
	}
	if err != nil {
		return err
	}

	_, err = io.WriteString(output, completionBridge[shell])
	return err
}

var completionBridge = map[string]string{
	"bash": `
# Complete the companion executable and Git's external-subcommand dispatch.
if [[ $(type -t compopt) = "builtin" ]]; then
    complete -o default -F __start_subcommit git-subcommit
else
    complete -o default -o nospace -F __start_subcommit git-subcommit
fi
_git_subcommit() {
    local COMP_WORDS=(git-subcommit "${COMP_WORDS[@]:$((__git_cmd_idx + 1))}")
    local COMP_CWORD=$((COMP_CWORD - __git_cmd_idx))
    __start_subcommit
}
`,
	"zsh": `
# Complete the companion executable and Zsh's Git external-subcommand dispatch.
compdef _subcommit git-subcommit
_git_subcommit() {
    _subcommit
}
`,
	"fish": `
# Complete the companion executable using the standalone command definition.
complete -c git-subcommit -w subcommit
`,
	"powershell": `
# Complete the companion executable using the same completer.
Register-ArgumentCompleter -CommandName 'git-subcommit' -ScriptBlock ${__subcommitCompleterBlock}
`,
}
