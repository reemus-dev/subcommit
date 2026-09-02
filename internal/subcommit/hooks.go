package subcommit

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/reemus-dev/subcommit/internal/console"
)

type hookSnapshot struct {
	targetWorktree map[string]worktreeState
	targetIndex    map[string]gitEntry
	allIndex       map[string]gitEntry
	worktree       map[string]dirtyWorktreeState
}

func (op *operation) snapshotHooks(ctx context.Context, selected selection) (hookSnapshot, error) {
	snapshot := hookSnapshot{
		targetWorktree: make(map[string]worktreeState, len(selected.targets)),
		targetIndex:    make(map[string]gitEntry, len(selected.targets)),
	}
	for _, target := range selected.targets {
		path := target.path
		value, err := op.currentWorktreeState(ctx, path)
		if err != nil {
			return snapshot, err
		}
		snapshot.targetWorktree[path] = value
		entry, err := op.candidateIndexEntry(ctx, path)
		if err != nil {
			return snapshot, err
		}
		snapshot.targetIndex[path] = entry
	}
	var err error
	snapshot.allIndex, err = op.candidateIndexEntries(ctx)
	if err != nil {
		return snapshot, err
	}
	snapshot.worktree, err = op.dirtyWorktree(ctx)
	return snapshot, err
}

func (op *operation) auditHooks(
	ctx context.Context, selected selection, before hookSnapshot,
) ([]string, error) {
	targets := make(map[string]bool, len(selected.targets))
	changedWorktree := make(map[string]bool)

	// Complete targets may be changed and restaged. Ranged targets are immutable.
	for _, target := range selected.targets {
		path := target.path
		targets[path] = true
		worktree, err := op.currentWorktreeState(ctx, path)
		if err != nil {
			return nil, err
		}
		index, err := op.candidateIndexEntry(ctx, path)
		if err != nil {
			return nil, err
		}
		if target.ranged {
			if worktree != before.targetWorktree[path] ||
				index != before.targetIndex[path] {
				return nil, &console.Diagnostic{
					Summary: fmt.Sprintf(
						"pre-commit changed a range-selected file: %s", safePreviewPath(path),
					),
					Hint: "disable that hook change or select the complete file without :ranges",
				}
			}
			continue
		}
		if worktree == before.targetWorktree[path] && index == before.targetIndex[path] {
			continue
		}
		if index.exists() && index == worktree.Entry {
			if worktree != before.targetWorktree[path] {
				changedWorktree[path] = true
			}
			continue
		}
		return nil, &console.Diagnostic{
			Summary: fmt.Sprintf(
				"pre-commit changed a selected file without restaging it: %s",
				safePreviewPath(path),
			),
			Hint: "update the hook to stage its final selected-file contents",
		}
	}

	// Hooks may not change any non-target candidate-index entry.
	afterIndex, err := op.candidateIndexEntries(ctx)
	if err != nil {
		return nil, err
	}
	for _, path := range unionKeys(before.allIndex, afterIndex) {
		if !targets[path] && before.allIndex[path] != afterIndex[path] {
			return nil, &console.Diagnostic{
				Summary: fmt.Sprintf(
					"pre-commit expanded the commit to an unselected path: %s",
					safePreviewPath(path),
				),
				Hint: "select that path explicitly or update the hook to leave it unchanged",
			}
		}
	}

	// Hooks may not change any non-target worktree status or contents.
	afterWorktree, err := op.dirtyWorktree(ctx)
	if err != nil {
		return nil, err
	}
	for _, path := range unionKeys(before.worktree, afterWorktree) {
		if !targets[path] && before.worktree[path] != afterWorktree[path] {
			return nil, &console.Diagnostic{
				Summary: fmt.Sprintf(
					"pre-commit changed an unselected worktree path: %s",
					safePreviewPath(path),
				),
				Hint: "select that path explicitly or update the hook to leave it unchanged",
			}
		}
	}
	return sortedKeys(changedWorktree), nil
}

func (op *operation) validateHookSelection(ctx context.Context, selected selection) error {
	var removed []string
	for _, target := range selected.targets {
		head, err := op.treeEntry(ctx, op.head, target.path)
		if err != nil {
			return err
		}
		candidate, err := op.candidateIndexEntry(ctx, target.path)
		if err != nil {
			return err
		}
		if candidate == head {
			removed = append(removed, safePreviewPath(target.path))
		}
	}
	if len(removed) > 0 {
		return &console.Diagnostic{
			Summary: "pre-commit removed all selected changes from a target",
			Hint:    "update the hook to preserve every requested target",
			Sections: []console.Section{{
				Title: "Removed targets",
				Lines: removed,
			}},
		}
	}

	for _, move := range op.exactMoves {
		source, err := op.candidateIndexEntry(ctx, move.source)
		if err != nil {
			return err
		}
		destination, err := op.candidateIndexEntry(ctx, move.destination)
		if err != nil {
			return err
		}
		if source.exists() || !destination.exists() {
			return &console.Diagnostic{
				Summary: fmt.Sprintf(
					"pre-commit no longer preserves the inferred move: %s -> %s",
					safePreviewPath(move.source), safePreviewPath(move.destination),
				),
				Hint: "update the hook to keep the source deleted and destination present",
			}
		}
	}
	return nil
}

func (op *operation) currentWorktreeState(ctx context.Context, path string) (worktreeState, error) {
	absolute := filepath.Join(op.root, filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	if os.IsNotExist(err) {
		return worktreeState{}, nil
	}
	if err != nil {
		return worktreeState{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(absolute)
		if err != nil {
			return worktreeState{}, err
		}
		digest, err := op.repoGit.OutputInput(ctx, []byte(target), "hash-object", "--stdin")
		if err != nil {
			return worktreeState{}, err
		}
		return worktreeState{
			Entry: gitEntry{
				Mode: "120000",
				OID:  string(bytes.TrimSpace(digest)),
			},
		}, nil
	}
	if info.IsDir() {
		return worktreeState{Directory: true}, nil
	}
	digest, err := op.repoGit.Output(ctx, "hash-object", "--", absolute)
	if err != nil {
		return worktreeState{}, err
	}
	headEntry, err := op.treeEntry(ctx, op.head, path)
	if err != nil {
		return worktreeState{}, err
	}
	return worktreeState{
		Entry: gitEntry{
			Mode: op.worktreeMode(info, headEntry),
			OID:  string(bytes.TrimSpace(digest)),
		},
	}, nil
}

func (op *operation) candidateIndexEntries(ctx context.Context) (map[string]gitEntry, error) {
	output, err := op.candidateGit.Output(ctx, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	entries := make(map[string]gitEntry)
	for _, record := range bytes.Split(output, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		tab := bytes.IndexByte(record, '\t')
		if tab < 0 {
			return nil, fmt.Errorf("invalid index record")
		}
		entry, ok := parseGitEntry(record[:tab], 1)
		if !ok {
			return nil, fmt.Errorf("invalid index record")
		}
		entries[string(record[tab+1:])] = entry
	}
	return entries, nil
}

func (op *operation) dirtyWorktree(ctx context.Context) (map[string]dirtyWorktreeState, error) {
	entries, err := op.statusEntries(ctx)
	if err != nil {
		return nil, err
	}
	values := make(map[string]dirtyWorktreeState)
	for _, entry := range entries {
		value, valueErr := op.currentWorktreeState(ctx, entry.Path)
		if valueErr != nil {
			return nil, valueErr
		}
		values[entry.Path] = dirtyWorktreeState{Status: entry.Code, Worktree: value}
	}
	return values, nil
}

func unionKeys[V comparable](left, right map[string]V) []string {
	all := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		all[key] = struct{}{}
	}
	for key := range right {
		all[key] = struct{}{}
	}
	return sortedKeys(all)
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
