package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/reemus-dev/subcommit/internal/cli"
)

var completions = []struct {
	shell string
	file  string
}{
	{shell: "bash", file: "subcommit.bash"},
	{shell: "zsh", file: "_subcommit"},
	{shell: "fish", file: "subcommit.fish"},
	{shell: "powershell", file: "subcommit.ps1"},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	const output = "bin/completions"
	if err := os.RemoveAll(output); err != nil {
		return err
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	for _, completion := range completions {
		var script bytes.Buffer
		if err := cli.GenerateCompletion(
			cli.NewRootCommand(), completion.shell, &script,
		); err != nil {
			return err
		}
		path := filepath.Join(output, completion.file)
		if err := os.WriteFile(path, script.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}
