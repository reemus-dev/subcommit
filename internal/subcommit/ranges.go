package subcommit

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/reemus-dev/subcommit/internal/console"
	"github.com/reemus-dev/subcommit/internal/patch"
)

func (op *operation) selectRanges(ctx context.Context, targets []Target) (selection, error) {
	var selected selection
	for _, target := range targets {
		path := target.Path
		absolute := filepath.Join(op.root, filepath.FromSlash(path))
		headEntry, err := op.treeEntry(ctx, op.head, path)
		if err != nil {
			return selection{}, err
		}
		info, statErr := os.Lstat(absolute)
		if statErr != nil {
			if os.IsNotExist(statErr) && headEntry.exists() {
				return selection{}, &console.Diagnostic{
					Summary: fmt.Sprintf(
						"range selection cannot commit a deleted file: %s",
						safePreviewPath(path),
					),
					Hint: "select the complete path without :ranges to commit the deletion",
				}
			}
			if os.IsNotExist(statErr) {
				return selection{}, &console.Diagnostic{
					Summary: fmt.Sprintf(
						"selected path does not exist: %s", safePreviewPath(path),
					),
					Hint: "check the path or remove it from the commit request",
				}
			}
			return selection{}, statErr
		}
		if info.IsDir() {
			return selection{}, directoryTargetError(path)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return selection{}, &console.Diagnostic{
				Summary: fmt.Sprintf(
					"range selection does not support symbolic links: %s",
					safePreviewPath(path),
				),
				Hint: "select the complete path without :ranges to commit the symbolic link",
			}
		}

		if !headEntry.exists() {
			return selection{}, &console.Diagnostic{
				Summary: fmt.Sprintf(
					"range selection requires a tracked file: %s", safePreviewPath(path),
				),
				Hint: "select the complete path without :ranges to commit a new file",
			}
		}

		diff, err := op.candidateGit.Output(
			ctx, "diff", "--no-color", op.head, "--", path,
		)
		if err != nil {
			return selection{}, err
		}
		if len(diff) == 0 {
			selected.skipped = append(selected.skipped, skippedPath{
				path:   path,
				reason: "no changes relative to the current commit",
			})
			continue
		}
		filtered, err := patch.Filter(diff, target.Ranges)
		if err != nil {
			return selection{}, err
		}
		if !bytes.Contains(filtered, []byte("@@")) {
			selected.skipped = append(selected.skipped, skippedPath{
				path:   path,
				reason: "selected ranges do not overlap changed hunks",
			})
			continue
		}

		changed, err := op.applyFilteredPatch(
			ctx, path, filtered, len(selected.targets),
		)
		if err != nil {
			return selection{}, err
		}
		if !changed {
			selected.skipped = append(selected.skipped, skippedPath{
				path:   path,
				reason: "selected ranges do not overlap changed hunks",
			})
			continue
		}

		selected.targets = append(selected.targets, selectedTarget{
			path:   path,
			ranged: true,
		})
	}

	return selected, nil
}

func (op *operation) applyFilteredPatch(
	ctx context.Context, path string,
	filtered []byte, patchNumber int,
) (bool, error) {
	before, err := op.candidateIndexEntry(ctx, path)
	if err != nil {
		return false, err
	}

	patchFile := filepath.Join(op.transactionDir, "patch-"+strconv.Itoa(patchNumber))
	if err := os.WriteFile(patchFile, filtered, 0o600); err != nil {
		return false, err
	}
	if err := op.candidateGit.Run(
		ctx, "apply", "--cached", "--recount", "--", patchFile,
	); err != nil {
		return false, fmt.Errorf("failed to apply filtered patch for %s: %w", path, err)
	}

	after, err := op.candidateIndexEntry(ctx, path)
	if err != nil {
		return false, err
	}
	return after != before, nil
}
