package subcommit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/reemus-dev/subcommit/internal/console"
)

func (op *operation) finalize(
	ctx context.Context, commit string, selected selection,
) (returnErr error) {
	headMoved := false
	defer func() {
		if returnErr != nil && !headMoved {
			returnErr = orphanCandidateError(returnErr, commit)
		}
	}()

	// Lock the real index before snapshotting staged state.
	indexPath := filepath.Join(op.gitDir, "index")
	lockPath := indexPath + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return indexLockedError(lockPath)
		}
		return detailedDiagnostic("cannot acquire Git's repository lock", err)
	}
	defer func() {
		if !headMoved {
			_ = lock.Close()
			_ = os.Remove(lockPath)
		}
	}()

	// Build the replacement from the locked index, changing only selected paths.
	prepared := filepath.Join(op.transactionDir, "prepared-index")
	preparedGit := op.repoGit.WithIndex(prepared)
	info, statErr := os.Stat(indexPath)
	if statErr == nil {
		if err := copyFile(indexPath, prepared, info.Mode().Perm()); err != nil {
			return err
		}
	} else if os.IsNotExist(statErr) {
		if err := preparedGit.Run(ctx, "read-tree", "--empty"); err != nil {
			return err
		}
		info, err = os.Stat(prepared)
		if err != nil {
			return err
		}
	} else {
		return statErr
	}

	for _, target := range selected.targets {
		path := target.path
		entry, err := op.treeEntry(ctx, commit, path)
		if err != nil {
			return err
		}
		if !entry.exists() {
			if err := preparedGit.Run(ctx, "update-index", "--force-remove", "--", path); err != nil {
				return err
			}
			continue
		}
		if err := preparedGit.Run(
			ctx, "update-index", "--add", "--cacheinfo", entry.cacheInfo(path),
		); err != nil {
			return err
		}
	}

	// Fully materialize and sync the lock before moving HEAD.
	preparedFile, err := os.Open(prepared)
	if err != nil {
		return err
	}
	if _, err := io.Copy(lock, preparedFile); err != nil {
		_ = preparedFile.Close()
		return fmt.Errorf("prepare index lock contents: %w", err)
	}
	if err := preparedFile.Close(); err != nil {
		return err
	}
	if err := os.Chmod(lockPath, info.Mode().Perm()); err != nil {
		return fmt.Errorf("set replacement index permissions: %w", err)
	}
	if err := syncFile(lock); err != nil {
		return fmt.Errorf("sync replacement index: %w", err)
	}

	// Move HEAD first, retaining recovery state until index publication succeeds.
	if err := op.repoGit.Run(ctx, "update-ref", "HEAD", commit, op.head); err != nil {
		current, verifyErr := op.repoGit.Output(
			context.WithoutCancel(ctx), "rev-parse", "--verify", "HEAD",
		)
		if verifyErr != nil || strings.TrimSpace(string(current)) != commit {
			return &console.Diagnostic{
				Summary: "the repository changed before the commit could be published",
				Hint:    "inspect the latest commit, then retry the scoped commit if needed",
			}
		}
	}
	headMoved = true
	op.retain = true
	if err := lock.Close(); err != nil {
		return op.publicationFailure(commit, lockPath, err)
	}
	if err := publishIndex(lockPath, indexPath); err != nil {
		return op.publicationFailure(commit, lockPath, err)
	}
	op.retain = false
	return nil
}

func orphanCandidateError(cause error, commit string) error {
	diagnostic := &console.Diagnostic{
		Summary: cause.Error(),
		Cause:   cause,
	}
	var existing *console.Diagnostic
	if errors.As(cause, &existing) {
		copy := *existing
		copy.Sections = append([]console.Section(nil), existing.Sections...)
		copy.Cause = cause
		diagnostic = &copy
	}
	diagnostic.Sections = append(diagnostic.Sections,
		console.Section{
			Title: "Candidate commit",
			Lines: []string{commit},
		},
		console.Section{
			Title: "State",
			Lines: []string{
				"The candidate commit was not published.",
				"subcommit did not rewrite the worktree.",
			},
		},
		console.Section{
			Title: "Recovery",
			Lines: []string{"git cherry-pick " + commit},
		},
	)
	return diagnostic
}

func (op *operation) publicationFailure(commit, lockPath string, cause error) error {
	return &console.Diagnostic{
		Summary: "the commit was created, but Git's selected-change state was not updated",
		Sections: []console.Section{
			{
				Title: "Commit",
				Lines: []string{commit},
			},
			{
				Title: "State",
				Lines: []string{
					"The commit is now current.",
					"subcommit did not rewrite the worktree.",
					"Recovery files were retained.",
				},
			},
			{
				Title: "Recovery files",
				Lines: []string{
					op.transactionDir,
					filepath.Join(op.transactionDir, "prepared-index"),
					lockPath,
				},
			},
			{
				Title: "Recovery",
				Lines: []string{
					"Inspect the retained files before continuing.",
					"Publish the retained lock as the Git index to preserve staged state.",
					"Or run: git reset --mixed " + commit,
					"The reset preserves worktree contents but rebuilds staged state.",
				},
			},
			{
				Title: "Cause",
				Lines: []string{cause.Error()},
			},
		},
		Cause: cause,
	}
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	if err := syncFile(output); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}

func syncFile(file *os.File) error {
	if err := file.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return err
	}
	return nil
}
