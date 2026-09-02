//go:build !windows

package console

func enableColor(int) bool {
	return true
}
