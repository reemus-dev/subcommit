package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

func main() {
	realGit := os.Getenv("SUBCOMMIT_REAL_GIT")
	if realGit == "" {
		fmt.Fprintln(os.Stderr, "SUBCOMMIT_REAL_GIT is required")
		os.Exit(2)
	}
	command := exec.Command(realGit, os.Args[1:]...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			os.Exit(exitError.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	barrierCommand := os.Getenv("SUBCOMMIT_GIT_PROXY_COMMAND")
	if barrierCommand == "" {
		barrierCommand = "update-ref"
	}
	for index := 1; index < len(os.Args); index++ {
		if os.Args[index] == barrierCommand {
			if err := barrier(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
			break
		}
	}
}

func barrier() error {
	ready := os.Getenv("SUBCOMMIT_GIT_PROXY_READY")
	release := os.Getenv("SUBCOMMIT_GIT_PROXY_RELEASE")
	if ready == "" || release == "" {
		return nil
	}
	if err := os.WriteFile(ready, nil, 0o644); err != nil {
		return err
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
	return fmt.Errorf("timed out waiting for %s", release)
}
