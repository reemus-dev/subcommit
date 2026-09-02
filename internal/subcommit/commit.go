package subcommit

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/reemus-dev/subcommit/internal/console"
)

func (op *operation) commit(ctx context.Context, selected selection) error {
	if err := os.WriteFile(op.messageFile, []byte(op.request.Message+"\n"), 0o600); err != nil {
		return err
	}

	// Snapshot around pre-commit because it may format selected files but must
	// not expand the requested scope.
	var hookChanges []string
	if !op.request.NoVerify {
		snapshot, err := op.snapshotHooks(ctx, selected)
		if err != nil {
			return err
		}
		if err := op.runHook(ctx, "pre-commit"); err != nil {
			return hookRejectedError("pre-commit", "the commit")
		}
		hookChanges, err = op.auditHooks(ctx, selected, snapshot)
		if err != nil {
			return err
		}
		if err := op.validateHookSelection(ctx, selected); err != nil {
			return err
		}
	}

	// Freeze the candidate tree before message hooks run.
	treeOutput, err := op.candidateGit.Output(ctx, "write-tree")
	if err != nil {
		return err
	}
	tree := strings.TrimSpace(string(treeOutput))
	headTree, err := op.repoGit.Output(ctx, "rev-parse", op.head+"^{tree}")
	if err != nil {
		return err
	}
	if tree == strings.TrimSpace(string(headTree)) {
		return nothingToCommitError(nil)
	}

	// Let message hooks finalize and validate the commit message.
	if err := op.runHook(ctx, "prepare-commit-msg", op.messageFile, "message"); err != nil {
		return hookRejectedError("prepare-commit-msg", "the commit message")
	}
	if !op.request.NoVerify {
		if err := op.runHook(ctx, "commit-msg", op.messageFile); err != nil {
			return hookRejectedError("commit-msg", "the commit message")
		}
	}
	message, err := os.ReadFile(op.messageFile)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(message)) == "" {
		return &console.Diagnostic{
			Summary: "the commit message is empty after hooks ran",
			Hint:    "update the message hook to leave a non-empty commit message",
		}
	}

	if err := op.previewAndConfirm(ctx, selected, message, hookChanges); err != nil {
		return err
	}

	commit, err := op.createCommitObject(ctx, tree)
	if err != nil {
		return err
	}

	// Publish HEAD and selected index entries under the real index lock.
	if err := op.finalize(ctx, commit, selected); err != nil {
		return err
	}

	if err := op.repoGit.Connected(
		ctx, op.output.Stdin(), op.output.Stderr(), op.output.Stderr(),
		"hook", "run", "--ignore-missing", "post-commit",
	); err != nil {
		op.output.Warning("post-commit hook failed, but the commit was still created")
	}
	op.output.Success(commit)
	return nil
}

func (op *operation) previewAndConfirm(
	ctx context.Context, selected selection, message []byte, hookChanges []string,
) error {
	stat, err := op.candidateGit.Output(
		ctx, "--no-pager", "diff", "--no-color", "--cached", op.head, "--stat",
	)
	if err != nil {
		return fmt.Errorf("render commit preview: %w", err)
	}
	var rangedPaths []string
	for _, target := range selected.targets {
		if target.ranged {
			rangedPaths = append(rangedPaths, target.path)
		}
	}
	var patch []byte
	if len(rangedPaths) > 0 {
		args := []string{
			"--no-pager", "diff", "--no-color", "--no-ext-diff", "--no-textconv",
			"--cached", op.head, "--",
		}
		patch, err = op.candidateGit.Output(ctx, append(args, rangedPaths...)...)
		if err != nil {
			return fmt.Errorf("render selected regions: %w", err)
		}
	}
	preserved, err := op.preservedPaths(ctx, selected)
	if err != nil {
		return err
	}

	displayHookChanges := make([]string, len(hookChanges))
	for index, path := range hookChanges {
		displayHookChanges[index] = safePreviewPath(path)
	}
	op.output.RenderPreview(console.Preview{
		Stat:             string(stat),
		Patch:            string(patch),
		Message:          string(message),
		Preserved:        preserved,
		HookChangedPaths: displayHookChanges,
		WillPrompt:       !op.request.Yes,
	})

	if op.request.Yes {
		return nil
	}
	confirmed, err := op.output.Confirm(ctx)
	if err != nil || !confirmed {
		return &console.Diagnostic{Summary: "commit canceled", Canceled: true}
	}
	return nil
}

func hookRejectedError(name, subject string) error {
	return &console.Diagnostic{
		Summary: fmt.Sprintf("%s hook rejected %s", name, subject),
		Hint:    "review the hook output above, fix the problem, then retry",
	}
}

func (op *operation) createCommitObject(ctx context.Context, tree string) (string, error) {
	args := []string{"commit-tree", tree, "-p", op.head, "-F", op.messageFile}
	if op.gpgSign {
		args = []string{"commit-tree", "-S", tree, "-p", op.head, "-F", op.messageFile}
	}
	output, err := op.repoGit.Output(ctx, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (op *operation) runHook(ctx context.Context, name string, args ...string) error {
	arguments := []string{"hook", "run", "--ignore-missing", name}
	if len(args) > 0 {
		arguments = append(arguments, "--")
		arguments = append(arguments, args...)
	}
	return op.candidateGit.Connected(
		ctx, op.output.Stdin(), op.output.Stderr(), op.output.Stderr(), arguments...,
	)
}
