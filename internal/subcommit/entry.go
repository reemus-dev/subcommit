package subcommit

import (
	"bytes"
	"context"
	"fmt"
)

type gitEntry struct {
	Mode string
	OID  string
}

func (e gitEntry) exists() bool {
	return e.Mode != "" && e.OID != ""
}

func (e gitEntry) cacheInfo(path string) string {
	return e.Mode + "," + e.OID + "," + path
}

func parseGitEntry(record []byte, oidField int) (gitEntry, bool) {
	fields := bytes.Fields(record)
	if len(fields) <= oidField {
		return gitEntry{}, false
	}
	entry := gitEntry{Mode: string(fields[0]), OID: string(fields[oidField])}
	return entry, entry.exists()
}

type worktreeState struct {
	Entry     gitEntry
	Directory bool
}

type dirtyWorktreeState struct {
	Status   string
	Worktree worktreeState
}

func (op *operation) candidateIndexEntry(ctx context.Context, path string) (gitEntry, error) {
	return op.indexEntry(ctx, true, path)
}

func (op *operation) realIndexEntry(ctx context.Context, path string) (gitEntry, error) {
	return op.indexEntry(ctx, false, path)
}

func (op *operation) indexEntry(ctx context.Context, candidate bool, path string) (gitEntry, error) {
	runner := op.repoGit
	if candidate {
		runner = op.candidateGit
	}
	output, err := runner.Output(ctx, "ls-files", "--stage", "-z", "--", path)
	if err != nil {
		return gitEntry{}, err
	}
	if len(output) == 0 {
		return gitEntry{}, nil
	}
	nul := bytes.IndexByte(output, 0)
	if nul < 0 {
		return gitEntry{}, fmt.Errorf("invalid index entry for %s", path)
	}
	entry, ok := parseGitEntry(output[:nul], 1)
	if !ok {
		return gitEntry{}, fmt.Errorf("invalid index entry for %s", path)
	}
	return entry, nil
}

func (op *operation) treeEntry(ctx context.Context, commit, path string) (gitEntry, error) {
	output, err := op.repoGit.Output(ctx, "ls-tree", "-z", commit, "--", path)
	if err != nil {
		return gitEntry{}, err
	}
	if len(output) == 0 {
		return gitEntry{}, nil
	}
	nul := bytes.IndexByte(output, 0)
	if nul < 0 {
		return gitEntry{}, fmt.Errorf("invalid tree entry for %s", path)
	}
	entry, ok := parseGitEntry(output[:nul], 2)
	if !ok {
		return gitEntry{}, fmt.Errorf("invalid tree entry for %s", path)
	}
	return entry, nil
}
