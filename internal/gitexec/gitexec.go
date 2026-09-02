// Package gitexec provides shell-free execution of the installed Git CLI.
package gitexec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Runner executes Git in one repository, optionally through an isolated index.
type Runner struct {
	// Dir is the working directory supplied to Git.
	Dir   string
	index string
}

// WithIndex returns a copy that binds subsequent commands to path.
func (r Runner) WithIndex(path string) Runner {
	r.index = path
	return r
}

// Output runs Git with literal path semantics and returns captured stdout.
func (r Runner) Output(ctx context.Context, args ...string) ([]byte, error) {
	return r.output(ctx, nil, args...)
}

// OutputInput runs Git with input on stdin and returns captured stdout.
func (r Runner) OutputInput(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	return r.output(ctx, bytes.NewReader(input), args...)
}

func (r Runner) output(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
	command := r.command(ctx, true, args...)
	command.Stdin = input
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), message)
	}
	return stdout.Bytes(), nil
}

// Run executes Git with literal path semantics and discards stdout.
func (r Runner) Run(ctx context.Context, args ...string) error {
	_, err := r.Output(ctx, args...)
	return err
}

// Connected runs Git with attached streams and the caller's pathspec environment.
// It is used for hooks whose subprocess behavior must remain caller-controlled.
func (r Runner) Connected(
	ctx context.Context, stdin io.Reader,
	stdout, stderr io.Writer, args ...string,
) error {
	command := r.command(ctx, false, args...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func (r Runner) command(ctx context.Context, literalPathspecs bool, args ...string) *exec.Cmd {
	full := append([]string{"-c", "diff.noprefix=false"}, args...)
	command := exec.CommandContext(ctx, "git", full...)
	command.Dir = r.Dir
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if key == "GIT_INDEX_FILE" {
			continue
		}
		if literalPathspecs {
			switch key {
			case "GIT_LITERAL_PATHSPECS",
				"GIT_GLOB_PATHSPECS",
				"GIT_NOGLOB_PATHSPECS",
				"GIT_ICASE_PATHSPECS":
				continue
			}
		}
		command.Env = append(command.Env, value)
	}
	if literalPathspecs {
		command.Env = append(command.Env, "GIT_LITERAL_PATHSPECS=1")
	}
	if r.index != "" {
		command.Env = append(command.Env, "GIT_INDEX_FILE="+r.index)
	}
	return command
}
