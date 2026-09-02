package subcommit

import (
	"bytes"
	"context"
)

type statusEntry struct {
	Code         string
	Path         string
	OriginalPath string
}

func (op *operation) statusEntries(ctx context.Context) ([]statusEntry, error) {
	output, err := op.repoGit.Output(
		ctx, "status", "--porcelain=v1", "--untracked-files=all", "-z",
	)
	if err != nil {
		return nil, err
	}
	return parseStatus(output), nil
}

func (op *operation) untrackedPaths(ctx context.Context) ([]string, error) {
	var paths []string
	for _, args := range [][]string{
		{"ls-files", "--others", "--exclude-standard", "-z"},
		{"ls-files", "--others", "--ignored", "--exclude-standard", "-z"},
	} {
		output, err := op.repoGit.Output(ctx, args...)
		if err != nil {
			return nil, err
		}
		for _, path := range bytes.Split(output, []byte{0}) {
			if len(path) > 0 {
				paths = append(paths, string(path))
			}
		}
	}
	return paths, nil
}

func parseStatus(output []byte) []statusEntry {
	records := bytes.Split(output, []byte{0})
	entries := make([]statusEntry, 0, len(records))
	for index := 0; index < len(records); index++ {
		record := records[index]
		if len(record) < 4 {
			continue
		}
		entry := statusEntry{Code: string(record[:2]), Path: string(record[3:])}
		if record[0] == 'R' || record[0] == 'C' || record[1] == 'R' || record[1] == 'C' {
			index++
			if index < len(records) {
				entry.OriginalPath = string(records[index])
			}
		}
		entries = append(entries, entry)
	}
	return entries
}
