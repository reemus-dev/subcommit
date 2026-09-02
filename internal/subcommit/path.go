package subcommit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reemus-dev/subcommit/internal/console"
)

func normalizePath(root, prefix, value string, ignoreCase bool) (string, error) {
	path := filepath.FromSlash(value)
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, filepath.FromSlash(prefix), path)
	}

	relative, err := filepath.Rel(root, path)
	if ignoreCase && (err != nil || outsideRoot(relative)) {
		var ok bool
		relative, ok = caseFoldRelative(root, path)
		if !ok {
			return "", outsideRepositoryError(value)
		}
	} else if err != nil || outsideRoot(relative) {
		return "", outsideRepositoryError(value)
	}

	if relative == "." {
		return "", directoryTargetError(value)
	}

	if ignoreCase {
		relative, err = canonicalizePathCase(root, relative)
		if err != nil {
			return "", err
		}
	}
	return filepath.ToSlash(relative), nil
}

func outsideRepositoryError(path string) error {
	return &console.Diagnostic{
		Summary: fmt.Sprintf("path is outside the repository: %s", safePreviewPath(path)),
		Hint:    "select a literal path inside the current repository",
	}
}

func directoryTargetError(path string) error {
	return &console.Diagnostic{
		Summary: fmt.Sprintf("cannot commit a directory: %s", safePreviewPath(path)),
		Hint:    "select one or more files inside the directory",
	}
}

func outsideRoot(path string) bool {
	return path == ".." ||
		strings.HasPrefix(path, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(path)
}

func caseFoldRelative(root, path string) (string, bool) {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rootVolume := filepath.VolumeName(root)
	pathVolume := filepath.VolumeName(path)

	if !strings.EqualFold(rootVolume, pathVolume) {
		return "", false
	}

	components := func(value, volume string) []string {
		value = strings.TrimPrefix(value, volume)
		value = strings.TrimLeft(value, string(filepath.Separator))
		if value == "" {
			return nil
		}
		return strings.Split(value, string(filepath.Separator))
	}

	rootParts := components(root, rootVolume)
	pathParts := components(path, pathVolume)

	if len(pathParts) < len(rootParts) {
		return "", false
	}

	for index := range rootParts {
		if !strings.EqualFold(rootParts[index], pathParts[index]) {
			return "", false
		}
	}

	if len(pathParts) == len(rootParts) {
		return ".", true
	}

	return filepath.Join(pathParts[len(rootParts):]...), true
}

func canonicalizePathCase(root, path string) (string, error) {
	parts := strings.Split(path, string(filepath.Separator))
	current := root

	for index, component := range parts {
		entries, err := os.ReadDir(current)

		if os.IsNotExist(err) {
			break
		}

		if err != nil {
			return "", err
		}

		var matched os.DirEntry
		for _, entry := range entries {
			if entry.Name() == component {
				matched = entry
				break
			}
			if matched == nil && strings.EqualFold(entry.Name(), component) {
				matched = entry
			}
		}

		if matched == nil {
			break
		}

		parts[index] = matched.Name()
		current = filepath.Join(current, matched.Name())

		if matched.Type()&os.ModeSymlink != 0 {
			break
		}
	}

	return filepath.Join(parts...), nil
}

func hasSymlinkParent(root, path string) (bool, error) {
	parent := filepath.Dir(filepath.FromSlash(path))
	if parent == "." {
		return false, nil
	}

	current := root
	for _, component := range strings.Split(parent, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return false, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true, nil
		}
	}

	return false, nil
}
