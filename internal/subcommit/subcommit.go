// Package subcommit selects and publishes scoped commits without using the real index
// as a selection input.
package subcommit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/reemus-dev/subcommit/internal/console"
	"github.com/reemus-dev/subcommit/internal/gitexec"
	"github.com/reemus-dev/subcommit/internal/patch"
)

// Range is an inclusive interval in new-file line coordinates.
type Range = patch.Range

// Target identifies a literal path. An empty Ranges slice selects the complete file.
type Target struct {
	Path   string
	Ranges []Range
}

// Request describes one scoped commit operation.
type Request struct {
	// Message is the initial content passed to message hooks.
	Message string
	// Yes skips interactive confirmation after hooks and preview generation.
	Yes bool
	// NoVerify skips pre-commit and commit-msg. Prepare-commit-msg still runs.
	NoVerify bool
	Targets  []Target
}

type skippedPath struct {
	path   string
	reason string
}

type selectedTarget struct {
	path   string
	ranged bool
}

type selection struct {
	targets []selectedTarget
	skipped []skippedPath
}

type operation struct {
	request        Request
	output         *console.Console
	repoGit        gitexec.Runner
	candidateGit   gitexec.Runner
	root           string
	gitDir         string
	head           string
	transactionDir string
	messageFile    string
	gpgSign        bool
	fileMode       bool
	exactMoves     []exactMove
	retain         bool
}

// Run selects changes from HEAD to the worktree, executes hooks, and publishes the commit.
// It aligns selected index entries to the new HEAD while preserving unrelated entries.
func Run(ctx context.Context, request Request, output *console.Console) error {
	op, err := setup(ctx, request)
	if err != nil {
		return err
	}
	op.output = output
	defer func() {
		if !op.retain {
			_ = os.RemoveAll(op.transactionDir)
		}
	}()

	if err := op.expandExactMoves(ctx); err != nil {
		return err
	}
	selected, err := op.selectTargets(ctx)
	if err != nil {
		return err
	}
	return op.commit(ctx, selected)
}

func setup(ctx context.Context, request Request) (*operation, error) {
	// Discover the repository before binding subsequent Git calls to its root.
	probe := gitexec.Runner{}
	rootOutput, err := probe.Output(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, &console.Diagnostic{
			Summary: "not a Git repository",
			Hint:    "run subcommit from inside the repository you want to commit",
		}
	}
	prefixOutput, err := probe.Output(ctx, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, detailedDiagnostic("cannot resolve the current repository path", err)
	}
	root := strings.TrimRight(string(rootOutput), "\r\n")
	prefix := strings.TrimRight(string(prefixOutput), "\r\n")
	repoGit := gitexec.Runner{Dir: root}
	gitDirOutput, err := repoGit.Output(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, detailedDiagnostic("cannot locate the repository's Git data", err)
	}
	gitDir := strings.TrimRight(string(gitDirOutput), "\r\n")

	// Refuse repository states that make a standalone commit unsafe.
	operationMarkers := []struct {
		marker    string
		operation string
	}{
		{marker: "MERGE_HEAD", operation: "merge"},
		{marker: "CHERRY_PICK_HEAD", operation: "cherry-pick"},
		{marker: "REVERT_HEAD", operation: "revert"},
		{marker: "BISECT_LOG", operation: "bisect"},
	}
	for _, item := range operationMarkers {
		if exists(filepath.Join(gitDir, item.marker)) {
			return nil, operationInProgressError(item.operation)
		}
	}
	if exists(filepath.Join(gitDir, "rebase-merge")) ||
		exists(filepath.Join(gitDir, "rebase-apply")) {
		return nil, operationInProgressError("rebase")
	}
	indexLock := filepath.Join(gitDir, "index.lock")
	if exists(indexLock) {
		return nil, indexLockedError(indexLock)
	}

	// Capture the HEAD and configuration that govern this operation.
	headOutput, err := repoGit.Output(ctx, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return nil, &console.Diagnostic{
			Summary: "the repository has no current commit",
			Hint:    "create the initial commit with Git before using subcommit",
		}
	}
	head := strings.TrimRight(string(headOutput), "\r\n")
	fileMode, err := readBoolConfig(ctx, repoGit, "core.fileMode", true)
	if err != nil {
		return nil, err
	}
	ignoreCase, err := readBoolConfig(ctx, repoGit, "core.ignorecase", false)
	if err != nil {
		return nil, err
	}
	gpgSign, err := readBoolConfig(ctx, repoGit, "commit.gpgsign", false)
	if err != nil {
		return nil, err
	}

	// Normalize and consolidate targets before creating temporary transaction state.
	request.Targets, err = normalizeTargets(root, prefix, request.Targets, ignoreCase)
	if err != nil {
		return nil, err
	}

	// Seed the isolated candidate index from the captured HEAD.
	transactionDir, err := os.MkdirTemp("", "subcommit-")
	if err != nil {
		return nil, detailedDiagnostic("cannot create temporary commit state", err)
	}
	candidateGit := repoGit.WithIndex(filepath.Join(transactionDir, "candidate-index"))
	op := &operation{
		request:        request,
		repoGit:        repoGit,
		candidateGit:   candidateGit,
		root:           root,
		gitDir:         gitDir,
		head:           head,
		transactionDir: transactionDir,
		messageFile:    filepath.Join(transactionDir, "COMMIT_EDITMSG"),
		gpgSign:        gpgSign,
		fileMode:       fileMode,
	}
	if err := candidateGit.Run(ctx, "read-tree", head); err != nil {
		_ = os.RemoveAll(transactionDir)
		return nil, err
	}
	return op, nil
}

func normalizeTargets(
	root, prefix string, targets []Target, ignoreCase bool,
) ([]Target, error) {
	consolidated := make([]Target, 0, len(targets))
	positions := make(map[string]int, len(targets))

	for _, target := range targets {
		normalized, err := normalizePath(root, prefix, target.Path, ignoreCase)
		if err != nil {
			return nil, err
		}
		target.Path = normalized

		position, exists := positions[normalized]
		if !exists {
			positions[normalized] = len(consolidated)
			consolidated = append(consolidated, target)
			continue
		}

		existing := &consolidated[position]
		existingComplete := len(existing.Ranges) == 0
		targetComplete := len(target.Ranges) == 0
		if existingComplete != targetComplete {
			return nil, &console.Diagnostic{
				Summary: fmt.Sprintf(
					"cannot select both a complete file and line ranges: %s",
					safePreviewPath(normalized),
				),
				Hint: "remove either the complete path or its :ranges target",
			}
		}
		if !existingComplete {
			existing.Ranges = append(existing.Ranges, target.Ranges...)
		}
	}

	return consolidated, nil
}

func (op *operation) selectTargets(ctx context.Context) (selection, error) {
	var complete, ranged []Target
	for _, target := range op.request.Targets {
		if len(target.Ranges) == 0 {
			complete = append(complete, target)
		} else {
			ranged = append(ranged, target)
		}
	}

	completeSelection, err := op.selectComplete(ctx, complete)
	if err != nil {
		return selection{}, err
	}
	rangeSelection, err := op.selectRanges(ctx, ranged)
	if err != nil {
		return selection{}, err
	}

	selected := selection{
		targets: append(completeSelection.targets, rangeSelection.targets...),
		skipped: append(completeSelection.skipped, rangeSelection.skipped...),
	}
	if len(selected.targets) == 0 {
		return selected, nothingToCommitError(selected.skipped)
	}
	if len(selected.skipped) > 0 {
		return selected, incompleteSelectionError(selected.skipped)
	}
	return selected, nil
}

func incompleteSelectionError(skipped []skippedPath) error {
	return &console.Diagnostic{
		Summary: "not every selected target contains a committable change",
		Hint:    "correct or remove the skipped targets, then retry",
		Sections: []console.Section{{
			Title: "Skipped targets",
			Lines: skippedRows(skipped),
		}},
	}
}

func detailedDiagnostic(summary string, cause error) *console.Diagnostic {
	return &console.Diagnostic{
		Summary: summary,
		Sections: []console.Section{{
			Title: "Details",
			Lines: []string{cause.Error()},
		}},
		Cause: cause,
	}
}

func operationInProgressError(operation string) error {
	return &console.Diagnostic{
		Summary: fmt.Sprintf(
			"cannot create a scoped commit while a %s is in progress", operation,
		),
		Hint: fmt.Sprintf("finish or abort the %s, then retry", operation),
	}
}

func indexLockedError(path string) error {
	return &console.Diagnostic{
		Summary: "Git is already updating the repository",
		Hint:    "wait for the other Git operation to finish, then retry",
		Sections: []console.Section{{
			Title: "Lock file",
			Lines: []string{safePreviewPath(path)},
		}},
	}
}

func readBoolConfig(
	ctx context.Context, runner gitexec.Runner,
	key string, defaultValue bool,
) (bool, error) {
	output, err := runner.Output(
		ctx, "config", "--bool",
		"--default="+strconv.FormatBool(defaultValue), "--get", key,
	)
	if err != nil {
		raw, rawErr := runner.Output(ctx, "config", "--get", key)
		if rawErr == nil {
			value := strings.TrimSpace(string(raw))
			return false, &console.Diagnostic{
				Summary: fmt.Sprintf(
					"Git configuration %s has invalid boolean value %q", key, value,
				),
				Hint:  fmt.Sprintf("set %s to true or false, then retry", key),
				Cause: err,
			}
		}
		return false, detailedDiagnostic(
			fmt.Sprintf("cannot read Git configuration %s", key), err,
		)
	}
	switch strings.TrimSpace(string(output)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, &console.Diagnostic{
			Summary: fmt.Sprintf("Git configuration %s is not a valid boolean", key),
			Hint:    fmt.Sprintf("set %s to true or false, then retry", key),
		}
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
