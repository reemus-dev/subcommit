package subcommit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/reemus-dev/subcommit/internal/console"
)

type moveKey struct {
	kind string
	oid  string
}

type moveShape struct {
	kind string
	size int64
}

type moveEndpoint struct {
	path string
	key  moveKey
}

type exactMove struct {
	source      string
	destination string
}

// expandExactMoves treats a unique exact-content deletion and addition as one
// complete-file target. Status discovers candidate paths, but HEAD and current
// worktree entries determine whether a pair exists.
func (op *operation) expandExactMoves(ctx context.Context) error {
	complete := make(map[string]bool, len(op.request.Targets))
	ranged := make(map[string]bool, len(op.request.Targets))
	candidates := make(map[string]bool, len(op.request.Targets))
	untrackedCandidates := make(map[string]bool)
	for _, target := range op.request.Targets {
		candidates[target.Path] = true
		if len(target.Ranges) == 0 {
			complete[target.Path] = true
		} else {
			ranged[target.Path] = true
		}
	}

	selectedKeys := make(map[moveKey]bool)
	selectedShapes := make(map[moveShape]bool)
	needsUntrackedCandidates := false
	for _, target := range op.request.Targets {
		if len(target.Ranges) != 0 {
			continue
		}
		head, err := op.treeEntry(ctx, op.head, target.Path)
		if err != nil {
			return err
		}
		worktree, err := op.currentWorktreeState(ctx, target.Path)
		if err != nil {
			return err
		}
		switch {
		case !head.exists() && worktree.Entry.exists():
			key := exactMoveKey(worktree.Entry)
			shape, err := op.worktreeMoveShape(target.Path, key.kind)
			if err != nil {
				return err
			}
			selectedKeys[key] = true
			selectedShapes[shape] = true
		case head.exists() && !worktree.Entry.exists() && !worktree.Directory:
			skipWorktree, err := op.isSkipWorktree(ctx, target.Path)
			if err != nil {
				return err
			}
			if !skipWorktree {
				key := exactMoveKey(head)
				size, err := op.objectSize(ctx, head.OID)
				if err != nil {
					return err
				}
				selectedKeys[key] = true
				selectedShapes[moveShape{kind: key.kind, size: size}] = true
				needsUntrackedCandidates = true
			}
		}
	}
	if len(selectedKeys) == 0 {
		return nil
	}

	entries, err := op.statusEntries(ctx)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		candidates[entry.Path] = true
		if entry.Code == "??" {
			untrackedCandidates[entry.Path] = true
		}
		if entry.OriginalPath != "" {
			candidates[entry.OriginalPath] = true
		}
	}
	if needsUntrackedCandidates {
		untracked, err := op.untrackedPaths(ctx)
		if err != nil {
			return err
		}
		for _, path := range untracked {
			candidates[path] = true
			untrackedCandidates[path] = true
		}
	}

	var additions, deletions []moveEndpoint
	var regularAdditions []string
	for path := range candidates {
		var head gitEntry
		if !untrackedCandidates[path] {
			var err error
			head, err = op.treeEntry(ctx, op.head, path)
			if err != nil {
				return err
			}
		}
		if head.exists() {
			_, statErr := os.Lstat(filepath.Join(op.root, filepath.FromSlash(path)))
			if statErr == nil {
				continue
			}
			if !os.IsNotExist(statErr) {
				return statErr
			}
			skipWorktree, err := op.isSkipWorktree(ctx, path)
			if err != nil {
				return err
			}
			key := exactMoveKey(head)
			if !skipWorktree && selectedKeys[key] {
				deletions = append(deletions, moveEndpoint{path: path, key: key})
			}
			continue
		}

		shape, relevant, err := op.candidateMoveShape(path, selectedShapes)
		if err != nil {
			return err
		}
		if !relevant {
			continue
		}
		if shape.kind == "regular" && !strings.Contains(path, "\n") {
			regularAdditions = append(regularAdditions, path)
			continue
		}
		worktree, err := op.currentWorktreeState(ctx, path)
		if err != nil {
			return err
		}
		key := exactMoveKey(worktree.Entry)
		if worktree.Entry.exists() && selectedKeys[key] {
			additions = append(additions, moveEndpoint{path: path, key: key})
		}
	}
	regularEndpoints, err := op.hashRegularMoveCandidates(ctx, regularAdditions, selectedKeys)
	if err != nil {
		return err
	}
	additions = append(additions, regularEndpoints...)

	additionsByKey := groupMoveEndpoints(additions)
	deletionsByKey := groupMoveEndpoints(deletions)
	var expanded []Target
	for _, target := range op.request.Targets {
		if len(target.Ranges) != 0 {
			continue
		}
		if endpoint, ok := findMoveEndpoint(additions, target.Path); ok {
			counterpart, err := exactMoveCounterpart(
				target.Path, endpoint.key,
				deletionsByKey, additionsByKey, complete, ranged,
			)
			if err != nil {
				return err
			}
			if counterpart != "" {
				expanded = append(expanded, Target{Path: counterpart})
				complete[counterpart] = true
				op.exactMoves = append(op.exactMoves, exactMove{
					source:      counterpart,
					destination: target.Path,
				})
			}
		}
		if endpoint, ok := findMoveEndpoint(deletions, target.Path); ok {
			counterpart, err := exactMoveCounterpart(
				target.Path, endpoint.key,
				additionsByKey, deletionsByKey, complete, ranged,
			)
			if err != nil {
				return err
			}
			if counterpart != "" {
				expanded = append(expanded, Target{Path: counterpart})
				complete[counterpart] = true
				op.exactMoves = append(op.exactMoves, exactMove{
					source:      target.Path,
					destination: counterpart,
				})
			}
		}
	}
	op.request.Targets = append(op.request.Targets, expanded...)
	return nil
}

func exactMoveKey(entry gitEntry) moveKey {
	kind := entry.Mode
	if regularFileMode(entry.Mode) {
		kind = "regular"
	}
	return moveKey{kind: kind, oid: entry.OID}
}

func (op *operation) objectSize(ctx context.Context, oid string) (int64, error) {
	output, err := op.repoGit.Output(ctx, "cat-file", "-s", oid)
	if err != nil {
		return 0, err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid Git object size for %s: %w", oid, err)
	}
	return size, nil
}

func (op *operation) worktreeMoveShape(path, kind string) (moveShape, error) {
	absolute := filepath.Join(op.root, filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	if err != nil {
		return moveShape{}, err
	}
	size, err := moveWorktreeSize(absolute, info)
	return moveShape{kind: kind, size: size}, err
}

func (op *operation) candidateMoveShape(
	path string, selected map[moveShape]bool,
) (moveShape, bool, error) {
	absolute := filepath.Join(op.root, filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	if os.IsNotExist(err) {
		return moveShape{}, false, nil
	}
	if err != nil {
		return moveShape{}, false, err
	}
	kind := "regular"
	if info.Mode()&os.ModeSymlink != 0 {
		kind = "120000"
	} else if !info.Mode().IsRegular() {
		return moveShape{}, false, nil
	}
	size, err := moveWorktreeSize(absolute, info)
	if err != nil {
		return moveShape{}, false, err
	}
	shape := moveShape{kind: kind, size: size}
	return shape, selected[shape], nil
}

func (op *operation) hashRegularMoveCandidates(
	ctx context.Context, paths []string, selected map[moveKey]bool,
) ([]moveEndpoint, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	input := []byte(strings.Join(paths, "\n") + "\n")
	output, err := op.repoGit.OutputInput(ctx, input, "hash-object", "--stdin-paths")
	if err != nil {
		return nil, err
	}
	oids := strings.Fields(string(output))
	if len(oids) != len(paths) {
		return nil, fmt.Errorf("Git returned %d hashes for %d move candidates", len(oids), len(paths))
	}
	var endpoints []moveEndpoint
	for index, path := range paths {
		key := moveKey{kind: "regular", oid: oids[index]}
		if selected[key] {
			endpoints = append(endpoints, moveEndpoint{path: path, key: key})
		}
	}
	return endpoints, nil
}

func moveWorktreeSize(absolute string, info os.FileInfo) (int64, error) {
	if info.Mode()&os.ModeSymlink == 0 {
		return info.Size(), nil
	}
	target, err := os.Readlink(absolute)
	return int64(len([]byte(target))), err
}

func groupMoveEndpoints(endpoints []moveEndpoint) map[moveKey][]string {
	grouped := make(map[moveKey][]string)
	for _, endpoint := range endpoints {
		grouped[endpoint.key] = append(grouped[endpoint.key], endpoint.path)
	}
	for key := range grouped {
		sort.Strings(grouped[key])
	}
	return grouped
}

func findMoveEndpoint(endpoints []moveEndpoint, path string) (moveEndpoint, bool) {
	for _, endpoint := range endpoints {
		if endpoint.path == path {
			return endpoint, true
		}
	}
	return moveEndpoint{}, false
}

func exactMoveCounterpart(
	selected string, key moveKey,
	counterparts, sameSide map[moveKey][]string,
	explicitComplete, ranged map[string]bool,
) (string, error) {
	matches := counterparts[key]
	for _, path := range matches {
		if explicitComplete[path] {
			return "", nil
		}
	}
	for _, path := range matches {
		if ranged[path] {
			return "", &console.Diagnostic{
				Summary: fmt.Sprintf(
					"cannot range-select one side of an exact move: %s",
					safePreviewPath(path),
				),
				Hint: "select the moved file as a complete path",
			}
		}
	}
	if len(matches) == 0 {
		return "", nil
	}
	if len(matches) == 1 && len(sameSide[key]) == 1 {
		return matches[0], nil
	}

	ambiguous := append(append([]string(nil), matches...), sameSide[key]...)
	sort.Strings(ambiguous)
	lines := make([]string, 0, len(ambiguous))
	for index, path := range ambiguous {
		if index == 0 || path != ambiguous[index-1] {
			lines = append(lines, safePreviewPath(path))
		}
	}
	return "", &console.Diagnostic{
		Summary: fmt.Sprintf(
			"cannot determine the exact move counterpart for: %s",
			safePreviewPath(selected),
		),
		Hint: "select the intended source and destination paths explicitly",
		Sections: []console.Section{{
			Title: "Matching paths",
			Lines: lines,
		}},
	}
}
