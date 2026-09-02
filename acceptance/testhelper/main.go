package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	hook := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	key := "SUBCOMMIT_HOOK_" + strings.ToUpper(strings.ReplaceAll(hook, "-", "_"))
	action := os.Getenv(key)

	var err error
	switch action {
	case "", "noop":
		return
	case "reject":
		os.Exit(1)
	case "print":
		fmt.Fprintln(os.Stdout, os.Getenv("SUBCOMMIT_HOOK_CONTENT"))
		return
	case "print-reject":
		fmt.Fprintln(os.Stdout, os.Getenv("SUBCOMMIT_HOOK_CONTENT"))
		fmt.Fprintln(os.Stderr, "post-commit rejected")
		os.Exit(1)
	case "barrier":
		err = barrier()
	case "record-cached":
		err = recordCached()
	case "record-pathspec-env":
		err = recordPathspecEnvironment()
	case "format-restage":
		err = formatAndRestage()
	case "chmod-executable":
		err = chmodExecutable()
	case "append-stage":
		err = appendAndStage()
	case "rewrite-message":
		err = rewriteMessage()
	case "append-message":
		err = appendMessage()
	case "touch":
		err = writeMarker(os.Getenv("SUBCOMMIT_HOOK_MARKER"), nil)
	default:
		err = fmt.Errorf("unknown hook helper action %q", action)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func barrier() error {
	if err := writeMarker(os.Getenv("SUBCOMMIT_HOOK_READY"), nil); err != nil {
		return err
	}
	release := os.Getenv("SUBCOMMIT_HOOK_RELEASE")
	if release == "" {
		return fmt.Errorf("SUBCOMMIT_HOOK_RELEASE is required")
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(release); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for release marker %s", release)
}

func recordCached() error {
	output, err := exec.Command("git", "diff", "--cached", "--name-only").Output()
	if err != nil {
		return fmt.Errorf("git diff --cached: %w", err)
	}
	return writeMarker(os.Getenv("SUBCOMMIT_HOOK_MARKER"), output)
}

func recordPathspecEnvironment() error {
	var content strings.Builder
	pathspecKeys := []string{
		"GIT_LITERAL_PATHSPECS",
		"GIT_GLOB_PATHSPECS",
		"GIT_NOGLOB_PATHSPECS",
		"GIT_ICASE_PATHSPECS",
	}
	for _, key := range pathspecKeys {
		value, ok := os.LookupEnv(key)
		if !ok {
			value = "<unset>"
		}
		fmt.Fprintf(&content, "%s=%s\n", key, value)
	}
	return writeMarker(os.Getenv("SUBCOMMIT_HOOK_MARKER"), []byte(content.String()))
}

func formatAndRestage() error {
	target := os.Getenv("SUBCOMMIT_HOOK_TARGET")
	if target == "" {
		return fmt.Errorf("SUBCOMMIT_HOOK_TARGET is required")
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(target, []byte(os.Getenv("SUBCOMMIT_HOOK_CONTENT")), mode); err != nil {
		return err
	}
	return runGit("add", "--", target)
}

func chmodExecutable() error {
	target := os.Getenv("SUBCOMMIT_HOOK_TARGET")
	if target == "" {
		return fmt.Errorf("SUBCOMMIT_HOOK_TARGET is required")
	}
	return os.Chmod(target, 0o755)
}

func appendAndStage() error {
	target := os.Getenv("SUBCOMMIT_HOOK_TARGET")
	if target == "" {
		return fmt.Errorf("SUBCOMMIT_HOOK_TARGET is required")
	}
	file, err := os.OpenFile(target, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(os.Getenv("SUBCOMMIT_HOOK_CONTENT"))
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return runGit("add", "--", target)
}

func rewriteMessage() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("message file argument is required")
	}
	message, err := os.ReadFile(os.Args[1])
	if err != nil {
		return err
	}
	prefix := os.Getenv("SUBCOMMIT_HOOK_CONTENT")
	return os.WriteFile(os.Args[1], []byte(prefix+strings.TrimSpace(string(message))+"\n"), 0o600)
}

func appendMessage() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("message file argument is required")
	}
	file, err := os.OpenFile(os.Args[1], os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(os.Getenv("SUBCOMMIT_HOOK_CONTENT"))
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func writeMarker(path string, content []byte) error {
	if path == "" {
		return fmt.Errorf("SUBCOMMIT_HOOK_MARKER is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func runGit(args ...string) error {
	command := exec.Command("git", args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
