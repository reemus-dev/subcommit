//go:build !windows

package subcommit

import "os"

func publishIndex(lockPath, indexPath string) error {
	return os.Rename(lockPath, indexPath)
}
