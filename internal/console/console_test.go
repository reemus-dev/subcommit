package console

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestColorPolicy(t *testing.T) {
	tests := []struct {
		name         string
		mode         ColorMode
		terminal     bool
		noColor      string
		terminalName string
		want         bool
	}{
		{name: "auto terminal", mode: ColorAuto, terminal: true, want: true},
		{name: "auto redirected", mode: ColorAuto, want: false},
		{name: "auto NO_COLOR", mode: ColorAuto, terminal: true, noColor: "1", want: false},
		{name: "auto dumb terminal", mode: ColorAuto, terminal: true, terminalName: "dumb", want: false},
		{name: "always overrides environment", mode: ColorAlways, noColor: "1", terminalName: "dumb", want: true},
		{name: "never", mode: ColorNever, terminal: true, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := colorEnabled(
				test.mode, test.terminal, test.noColor, test.terminalName,
			)
			if got != test.want {
				t.Fatalf("colorEnabled() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestConsoleRendersPlainPreviewAndStableSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	output, err := New(
		strings.NewReader(""), &stdout, &stderr,
		Options{Color: ColorNever},
	)
	if err != nil {
		t.Fatal(err)
	}

	output.RenderPreview(Preview{
		Stat:      " a.txt | 2 +-\n 1 file changed, 1 insertion(+), 1 deletion(-)\n",
		Patch:     "diff --git a/a.txt b/a.txt\n@@ -1 +1 @@\n-old\n+new\n",
		Message:   "update a\n\nExplain why.\n",
		Preserved: []string{"?? note.txt", "MM b.txt"},
	})
	output.Success("0123456789abcdef")

	wantStderr := "Commit preview\n\n" +
		"Changes\n" +
		"   a.txt | 2 +-\n" +
		"   1 file changed, 1 insertion(+), 1 deletion(-)\n\n" +
		"Selected regions\n" +
		"  diff --git a/a.txt b/a.txt\n" +
		"  @@ -1 +1 @@\n" +
		"  -old\n" +
		"  +new\n\n" +
		"Message\n" +
		"  update a\n" +
		"  \n" +
		"  Explain why.\n\n" +
		"Other changes (2 paths, not committed)\n" +
		"  ?? note.txt\n" +
		"  MM b.txt\n\n"
	if stderr.String() != wantStderr {
		t.Fatalf("stderr:\n%s\nwant:\n%s", stderr.String(), wantStderr)
	}
	if stdout.String() != "committed: 0123456789abcdef\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestConsoleColorizesPreviewDiff(t *testing.T) {
	var stderr bytes.Buffer
	output, err := New(
		strings.NewReader(""), &bytes.Buffer{}, &stderr,
		Options{Color: ColorAlways},
	)
	if err != nil {
		t.Fatal(err)
	}

	output.RenderPreview(Preview{Patch: "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n unchanged\n"})

	got := stderr.String()
	for _, want := range []string{
		"  diff --git a/a.txt b/a.txt\n",
		"  \x1b[31m--- a/a.txt\x1b[0m\n",
		"  \x1b[32m+++ b/a.txt\x1b[0m\n",
		"  \x1b[36m@@ -1 +1 @@\x1b[0m\n",
		"  \x1b[31m-old\x1b[0m\n",
		"  \x1b[32m+new\x1b[0m\n",
		"   unchanged\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stderr missing %q:\n%s", want, got)
		}
	}
}

func TestQuietConsoleStillDisclosesHookChanges(t *testing.T) {
	var stderr bytes.Buffer
	output, err := New(
		strings.NewReader(""), &bytes.Buffer{}, &stderr,
		Options{Color: ColorNever, Quiet: true},
	)
	if err != nil {
		t.Fatal(err)
	}

	output.RenderPreview(Preview{
		Stat:             "hidden",
		Message:          "hidden",
		Preserved:        []string{"hidden"},
		HookChangedPaths: []string{"a.txt"},
		WillPrompt:       true,
	})
	if strings.Contains(stderr.String(), "hidden") ||
		!strings.Contains(stderr.String(), "Canceling will not undo") {
		t.Fatalf("quiet hook output = %q", stderr.String())
	}
}

func TestConsoleRendersDiagnostic(t *testing.T) {
	var stderr bytes.Buffer
	output, err := New(
		strings.NewReader(""), &bytes.Buffer{}, &stderr,
		Options{Color: ColorNever},
	)
	if err != nil {
		t.Fatal(err)
	}

	output.RenderError(&Diagnostic{
		Summary: "cannot commit",
		Usage:   "subcommit <path|path:ranges>... --message <message>",
		Hint:    "retry later",
		Sections: []Section{{
			Title: "State",
			Lines: []string{"no commit was created"},
		}},
	})

	want := "error: cannot commit\n" +
		"usage: subcommit <path|path:ranges>... --message <message>\n" +
		"hint: retry later\n\n" +
		"State\n" +
		"  no commit was created\n"
	if stderr.String() != want {
		t.Fatalf("diagnostic = %q, want %q", stderr.String(), want)
	}
}

func TestConsoleRendersCancellationWithoutErrorLabel(t *testing.T) {
	var stderr bytes.Buffer
	output, err := New(
		strings.NewReader(""), &bytes.Buffer{}, &stderr,
		Options{Color: ColorNever},
	)
	if err != nil {
		t.Fatal(err)
	}
	output.RenderError(&Diagnostic{Summary: "commit canceled", Canceled: true})
	if stderr.String() != "commit canceled\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestConsoleFallsBackToRawError(t *testing.T) {
	var stderr bytes.Buffer
	output, err := New(
		strings.NewReader(""), &bytes.Buffer{}, &stderr,
		Options{Color: ColorNever},
	)
	if err != nil {
		t.Fatal(err)
	}
	output.RenderError(errors.New("boom"))
	if stderr.String() != "error: boom\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestConsoleConfirmation(t *testing.T) {
	for _, test := range []struct {
		reply string
		want  bool
	}{
		{reply: "y\n", want: true},
		{reply: "Y\n", want: true},
		{reply: "yes\n", want: true},
		{reply: "YES\n", want: true},
		{reply: "n\n", want: false},
		{reply: "", want: false},
	} {
		var stderr bytes.Buffer
		output, err := New(
			strings.NewReader(test.reply), &bytes.Buffer{}, &stderr,
			Options{Color: ColorNever},
		)
		if err != nil {
			t.Fatal(err)
		}
		got, err := output.Confirm(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Errorf("Confirm(%q) = %t, want %t", test.reply, got, test.want)
		}
	}
}

func TestConsoleConfirmationHonorsCancellation(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	output, err := New(reader, &bytes.Buffer{}, &bytes.Buffer{}, Options{Color: ColorNever})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	confirmed, err := output.Confirm(ctx)
	if confirmed || !errors.Is(err, context.Canceled) {
		t.Fatalf("Confirm() = %t, %v", confirmed, err)
	}
}

func TestDevNullIsNotPromptable(t *testing.T) {
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := New(input, &bytes.Buffer{}, input, Options{Color: ColorNever})
	if err != nil {
		t.Fatal(err)
	}
	if output.CanPrompt() {
		t.Fatal("device null was treated as a promptable terminal")
	}
}
