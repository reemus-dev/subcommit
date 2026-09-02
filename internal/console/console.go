// Package console owns terminal interaction and renders all process-facing output.
package console

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"golang.org/x/term"
)

const defaultPreservedLimit = 12

// ColorMode controls ANSI styling for human-facing output.
type ColorMode string

const (
	// ColorAuto enables color only for a capable terminal.
	ColorAuto ColorMode = "auto"
	// ColorAlways enables color regardless of terminal detection.
	ColorAlways ColorMode = "always"
	// ColorNever disables color.
	ColorNever ColorMode = "never"
)

// Options controls presentation without changing the stable stdout record.
type Options struct {
	Color ColorMode
	// Quiet suppresses ordinary preview output but not hook-change disclosure.
	Quiet bool
	// Verbose disables preview truncation.
	Verbose bool
}

// Preview describes the candidate commit and state that will remain uncommitted.
type Preview struct {
	Stat      string
	Patch     string
	Message   string
	Preserved []string
	// HookChangedPaths lists worktree paths modified by pre-commit.
	HookChangedPaths []string
	// WillPrompt enables guidance about changes that canceling cannot undo.
	WillPrompt bool
}

// Section groups related diagnostic details under a heading.
type Section struct {
	Title string
	Lines []string
}

// Diagnostic carries structured user-facing error and recovery information.
// Cause supports errors.Is and errors.As. Canceled suppresses the error label.
type Diagnostic struct {
	Summary  string
	Hint     string
	Usage    string
	Sections []Section
	Cause    error
	Canceled bool
}

func (diagnostic *Diagnostic) Error() string {
	return diagnostic.Summary
}

func (diagnostic *Diagnostic) Unwrap() error {
	return diagnostic.Cause
}

// Console is the single gateway for prompts, previews, diagnostics, and stable output.
type Console struct {
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
	options Options
	color   bool
}

// New creates a console after validating its presentation options.
func New(stdin io.Reader, stdout, stderr io.Writer, options Options) (*Console, error) {
	if !options.Color.valid() {
		return nil, &Diagnostic{
			Summary: fmt.Sprintf("invalid color mode %q", options.Color),
			Hint:    "use auto, always, or never",
		}
	}
	if options.Quiet && options.Verbose {
		return nil, &Diagnostic{Summary: "--quiet and --verbose cannot be used together"}
	}

	color := resolveColor(options.Color, stderr)
	return &Console{
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		options: options,
		color:   color,
	}, nil
}

func (console *Console) Stdin() io.Reader {
	return console.stdin
}

func (console *Console) Stderr() io.Writer {
	return console.stderr
}

// CanPrompt reports whether both prompt input and human-facing output are terminals.
func (console *Console) CanPrompt() bool {
	return isTerminalStream(console.stdin) && isTerminalStream(console.stderr)
}

// Confirm asks whether to publish and stops waiting when ctx is canceled.
func (console *Console) Confirm(ctx context.Context) (bool, error) {
	fmt.Fprintln(console.stderr)
	fmt.Fprint(console.stderr, console.style(styleBold, "Publish this commit?"), " [y/N] ")

	result := make(chan bool, 1)
	go func() {
		var reply string
		_, _ = fmt.Fscanln(console.stdin, &reply)
		result <- strings.HasPrefix(strings.ToLower(reply), "y")
	}()

	select {
	case confirmed := <-result:
		return confirmed, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// RenderPreview writes human-facing commit state while honoring quiet and verbose modes.
// Hook changes remain visible in quiet mode because canceling cannot undo them.
func (console *Console) RenderPreview(preview Preview) {
	if !console.options.Quiet {
		console.heading("Commit preview")
		console.section("Changes", lines(preview.Stat))
		console.diffSection("Selected regions", lines(preview.Patch))
		console.section("Message", messageLines(preview.Message))
		console.renderPreserved(preview.Preserved)
	}
	console.renderHookChanges(preview.HookChangedPaths, preview.WillPrompt)
}

// Success writes the stable, uncolored commit record to stdout.
func (console *Console) Success(commit string) {
	if !console.options.Quiet {
		fmt.Fprintln(console.stderr)
	}
	fmt.Fprintf(console.stdout, "committed: %s\n", commit)
}

// Warning writes a human-facing warning to stderr.
func (console *Console) Warning(message string) {
	fmt.Fprintf(
		console.stderr,
		"%s %s\n",
		console.style(styleYellow|styleBold, "warning:"),
		message,
	)
}

// RenderError writes a structured diagnostic or falls back to the raw error text.
func (console *Console) RenderError(err error) {
	var diagnostic *Diagnostic
	if !errors.As(err, &diagnostic) {
		diagnostic = &Diagnostic{Summary: err.Error()}
	}

	if diagnostic.Canceled {
		fmt.Fprintln(console.stderr, console.style(styleYellow, diagnostic.Summary))
		return
	}

	fmt.Fprintf(
		console.stderr,
		"%s %s\n",
		console.style(styleRed|styleBold, "error:"),
		diagnostic.Summary,
	)
	if diagnostic.Usage != "" {
		fmt.Fprintf(console.stderr, "%s %s\n", console.style(styleBold, "usage:"), diagnostic.Usage)
	}
	if diagnostic.Hint != "" {
		fmt.Fprintf(
			console.stderr,
			"%s %s\n",
			console.style(styleYellow|styleBold, "hint:"),
			diagnostic.Hint,
		)
	}
	for _, section := range diagnostic.Sections {
		console.section(section.Title, section.Lines)
	}
}

func (console *Console) renderPreserved(preserved []string) {
	if len(preserved) == 0 {
		return
	}

	preserved = append([]string(nil), preserved...)
	sort.Strings(preserved)
	shown := preserved
	if !console.options.Verbose && len(shown) > defaultPreservedLimit {
		shown = shown[:defaultPreservedLimit]
	}
	noun := "paths"
	if len(preserved) == 1 {
		noun = "path"
	}
	title := fmt.Sprintf("Other changes (%d %s, not committed)", len(preserved), noun)
	rows := append([]string(nil), shown...)
	if remaining := len(preserved) - len(shown); remaining > 0 {
		rows = append(rows, fmt.Sprintf("... %d more. Use --verbose to list all", remaining))
	}
	console.section(title, rows)
}

func (console *Console) renderHookChanges(paths []string, willPrompt bool) {
	if len(paths) == 0 {
		return
	}

	sort.Strings(paths)
	noun := "files"
	if len(paths) == 1 {
		noun = "file"
	}
	rows := []string{fmt.Sprintf(
		"pre-commit updated and restaged %d selected %s:", len(paths), noun,
	)}
	for _, path := range paths {
		rows = append(rows, "  "+path)
	}
	if willPrompt {
		rows = append(rows, "Canceling will not undo these worktree changes.")
	}
	console.section("Hook changes", rows)
}

func (console *Console) heading(title string) {
	fmt.Fprintln(console.stderr, console.style(styleCyan|styleBold, title))
}

func (console *Console) section(title string, rows []string) {
	if len(rows) == 0 {
		return
	}

	fmt.Fprintln(console.stderr)
	fmt.Fprintln(console.stderr, console.style(styleBold, title))
	for _, row := range rows {
		fmt.Fprintln(console.stderr, "  "+row)
	}
}

func (console *Console) diffSection(title string, rows []string) {
	if len(rows) == 0 {
		return
	}

	fmt.Fprintln(console.stderr)
	fmt.Fprintln(console.stderr, console.style(styleBold, title))
	for _, row := range rows {
		style := styleCode(0)
		switch {
		case strings.HasPrefix(row, "+"):
			style = styleGreen
		case strings.HasPrefix(row, "-"):
			style = styleRed
		case strings.HasPrefix(row, "@@"):
			style = styleCyan
		}
		fmt.Fprintln(console.stderr, "  "+console.style(style, row))
	}
}

func lines(value string) []string {
	value = strings.TrimRight(value, "\r\n")
	if value == "" {
		return nil
	}
	return strings.Split(value, "\n")
}

func messageLines(message string) []string {
	rows := lines(message)
	for index := range rows {
		rows[index] = strings.TrimSuffix(rows[index], "\r")
	}
	return rows
}

func isTerminalStream(stream any) bool {
	file, ok := stream.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(int(file.Fd()))
}
