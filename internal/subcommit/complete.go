package subcommit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/reemus-dev/subcommit/internal/console"
)

func (op *operation) selectComplete(ctx context.Context, targets []Target) (selection, error) {
	var selected selection
	for _, target := range targets {
		path := target.Path
		absolute := filepath.Join(op.root, filepath.FromSlash(path))
		info, statErr := os.Lstat(absolute)
		if statErr == nil && info.IsDir() {
			return selection{}, directoryTargetError(path)
		}
		headEntry, err := op.treeEntry(ctx, op.head, path)
		if err != nil {
			return selection{}, err
		}

		switch {
		case statErr == nil:
			if !headEntry.exists() {
				beyondSymlink, err := hasSymlinkParent(op.root, path)
				if err != nil {
					return selection{}, err
				}
				if beyondSymlink {
					return selection{}, &console.Diagnostic{
						Summary: fmt.Sprintf(
							"cannot add an untracked file through a symbolic-link directory: %s",
							safePreviewPath(path),
						),
						Hint: "select the file through its real repository path",
					}
				}
			}
			entry, err := op.materializeWorktreeEntry(ctx, absolute, info, headEntry)
			if err != nil {
				return selection{}, err
			}

			if headEntry == entry {
				indexEntry, err := op.realIndexEntry(ctx, path)
				if err != nil {
					return selection{}, err
				}
				if indexEntry != headEntry {
					return selection{}, &console.Diagnostic{
						Summary: fmt.Sprintf(
							"selected path has staged changes absent from its worktree: %s",
							safePreviewPath(path),
						),
						Hint: "restore or unstage those changes, or remove the path from the request",
					}
				}
				selected.skipped = append(selected.skipped, skippedPath{
					path:   path,
					reason: "no changes relative to the current commit",
				})
				continue
			}
			selected.targets = append(selected.targets, selectedTarget{path: path})
			if err := op.candidateGit.Run(
				ctx, "update-index", "--add", "--cacheinfo", entry.cacheInfo(path),
			); err != nil {
				return selection{}, err
			}
		case !os.IsNotExist(statErr):
			return selection{}, statErr
		case headEntry.exists():
			skipped, err := op.isSkipWorktree(ctx, path)
			if err != nil {
				return selection{}, err
			}
			if skipped {
				selected.skipped = append(selected.skipped, skippedPath{
					path:   path,
					reason: "absent from this worktree and treated as unchanged",
				})
				continue
			}
			if err := op.candidateGit.Run(ctx, "update-index", "--force-remove", "--", path); err != nil {
				return selection{}, err
			}
			selected.targets = append(selected.targets, selectedTarget{path: path})
		default:
			return selection{}, &console.Diagnostic{
				Summary: fmt.Sprintf(
					"selected path does not exist and is not tracked: %s",
					safePreviewPath(path),
				),
				Hint: "check the path or remove it from the commit request",
			}
		}
	}

	return selected, nil
}

func (op *operation) materializeWorktreeEntry(
	ctx context.Context, absolute string,
	info os.FileInfo, headEntry gitEntry,
) (gitEntry, error) {
	var blob []byte
	var err error
	if info.Mode()&os.ModeSymlink != 0 {
		target, readErr := os.Readlink(absolute)
		if readErr != nil {
			return gitEntry{}, readErr
		}
		blob, err = op.repoGit.OutputInput(ctx, []byte(target), "hash-object", "-w", "--stdin")
	} else {
		blob, err = op.repoGit.Output(ctx, "hash-object", "-w", "--", absolute)
	}
	if err != nil {
		return gitEntry{}, err
	}
	return gitEntry{
		Mode: op.worktreeMode(info, headEntry),
		OID:  strings.TrimSpace(string(blob)),
	}, nil
}

func (op *operation) isSkipWorktree(ctx context.Context, path string) (bool, error) {
	output, err := op.repoGit.Output(ctx, "ls-files", "--stage", "-v", "-z", "--", path)
	if err != nil {
		return false, err
	}
	return len(output) > 0 && (output[0] == 'S' || output[0] == 's'), nil
}

func (op *operation) worktreeMode(info os.FileInfo, headEntry gitEntry) string {
	if info.Mode()&os.ModeSymlink != 0 {
		return "120000"
	}
	if op.fileMode {
		return fileMode(info)
	}
	if regularFileMode(headEntry.Mode) {
		return headEntry.Mode
	}
	return "100644"
}

func regularFileMode(mode string) bool {
	return mode == "100644" || mode == "100755"
}

func fileMode(info os.FileInfo) string {
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 != 0 {
		return "100755"
	}
	return "100644"
}
