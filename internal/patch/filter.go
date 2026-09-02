// Package patch filters unified diffs by new-file line ranges.
package patch

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
)

// Range is an inclusive interval in new-file line coordinates.
type Range struct {
	Start int
	End   int
}

var hunkStart = regexp.MustCompile(`^@@ -[0-9]+(?:,[0-9]+)? \+([0-9]+)(?:,[0-9]+)? @@`)

// Filter keeps complete contiguous change regions that overlap ranges.
// Excluded deletions become context so the result remains an applicable patch.
func Filter(input []byte, ranges []Range) ([]byte, error) {
	var output [][]byte
	newLine := 0
	lastContext := 0
	inHunk := false
	var group [][]byte
	var positions []int

	selected := func(line int) bool {
		for _, target := range ranges {
			if line >= target.Start && line <= target.End {
				return true
			}
		}
		return false
	}

	flush := func() {
		if len(group) == 0 {
			return
		}
		hit := false

		if len(positions) > 0 {
			for _, position := range positions {
				if selected(position) {
					hit = true
					break
				}
			}
		} else {
			hit = selected(newLine) || lastContext > 0 && selected(lastContext)
		}

		for index, line := range group {
			if hit {
				output = append(output, line)
				continue
			}
			switch line[0] {
			case '-':
				demoted := append([]byte(nil), line...)
				demoted[0] = ' '
				output = append(output, demoted)
			case '\\':
				if index > 0 && group[index-1][0] == '-' {
					output = append(output, line)
				}
			}
		}

		group = nil
		positions = nil
	}

	for line := range bytes.Lines(input) {
		plain := bytes.TrimSuffix(line, []byte{'\n'})
		plain = bytes.TrimSuffix(plain, []byte{'\r'})

		if match := hunkStart.FindSubmatch(plain); match != nil {
			flush()
			value, err := strconv.Atoi(string(match[1]))
			if err != nil {
				return nil, fmt.Errorf("parse hunk header: %w", err)
			}
			newLine = value
			lastContext = 0
			inHunk = true
			output = append(output, line)
			continue
		}

		if !inHunk {
			output = append(output, line)
			continue
		}

		switch line[0] {
		case ' ':
			flush()
			output = append(output, line)
			lastContext = newLine
			newLine++
		case '-':
			group = append(group, line)
		case '+':
			group = append(group, line)
			positions = append(positions, newLine)
			newLine++
		case '\\':
			if len(group) > 0 {
				group = append(group, line)
			} else {
				output = append(output, line)
			}
		default:
			flush()
			inHunk = false
			output = append(output, line)
		}
	}

	flush()
	return bytes.Join(output, nil), nil
}
