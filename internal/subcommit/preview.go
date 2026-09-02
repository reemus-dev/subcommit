package subcommit

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/reemus-dev/subcommit/internal/console"
)

func (op *operation) preservedPaths(
	ctx context.Context, selected selection,
) ([]string, error) {
	entries, err := op.statusEntries(ctx)
	if err != nil {
		return nil, err
	}
	targets := make(map[string]bool, len(selected.targets))
	for _, target := range selected.targets {
		targets[target.path] = true
	}

	var preserved []string
	for _, entry := range entries {
		display := safePreviewPath(entry.Path)
		if entry.OriginalPath != "" {
			display = safePreviewPath(entry.OriginalPath) + " -> " + display
		}
		if !targets[entry.Path] {
			preserved = append(preserved, fmt.Sprintf("%s %s", entry.Code, display))
		}
	}
	sort.Strings(preserved)
	return preserved, nil
}

func skippedRows(skipped []skippedPath) []string {
	rows := make([]string, 0, len(skipped))
	for _, item := range skipped {
		rows = append(rows, safePreviewPath(item.path)+"  "+item.reason)
	}
	return rows
}

func nothingToCommitError(skipped []skippedPath) error {
	lines := skippedRows(skipped)
	diagnostic := &console.Diagnostic{Summary: "nothing to commit"}
	if len(lines) > 0 {
		diagnostic.Sections = []console.Section{{
			Title: "Selected paths",
			Lines: lines,
		}}
	}
	return diagnostic
}

func safePreviewPath(path string) string {
	if !utf8.ValidString(path) || strings.ContainsAny(path, "\x00\r\n\t") {
		return strconv.QuoteToASCII(path)
	}
	return path
}
