//go:build windows

package subcommit

import (
	"errors"
	"os"
	"syscall"
	"time"
)

const (
	windowsSharingViolation = syscall.Errno(32)
	publicationAttempts     = 10
)

func publishIndex(lockPath, indexPath string) error {
	delay := 10 * time.Millisecond
	var err error
	for attempt := 0; attempt < publicationAttempts; attempt++ {
		err = os.Rename(lockPath, indexPath)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) && !errors.Is(err, windowsSharingViolation) {
			return err
		}
		if attempt+1 < publicationAttempts {
			time.Sleep(delay)
			if delay < 160*time.Millisecond {
				delay *= 2
			}
		}
	}
	return err
}
