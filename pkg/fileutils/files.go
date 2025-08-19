package fileutils

import (
	"encoding/json"
	"fmt"
	defs "mica-shim/definitions"
	"os"
	"path/filepath"
	"syscall"

	log "mica-shim/logger"
)

func IsSymlink(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeSymlink != 0
}

func ResolvePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path must be specified")
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file does not exist: %s", absolute)
		}

		return "", err
	}

	return resolved, nil
}

// getAllParentPaths returns all the parent directories of a path, including itself but excluding root directory "/".
// For example, "/foo/bar/biz" returns {"/foo", "/foo/bar", "/foo/bar/biz"}
func getAllParentPaths(path string) []string {
	if path == "/" || path == "." {
		return []string{}
	}

	paths := []string{filepath.Clean(path)}
	cur := path
	var parent string
	for cur != "/" && cur != "." {
		parent = filepath.Dir(cur)
		paths = append([]string{parent}, paths...)
		cur = parent
	}
	// remove the "/" or "." from the return result
	return paths[1:]
}

// MkdirAllWithInheritedOwner creates a directory named path, along with any necessary parents.
// It creates the missing directories with the ownership of the last existing parent.
// The path needs to be absolute and the method doesn't handle symlink.
func MkdirAllWithInheritedOwner(path string, perm os.FileMode) error {
	if len(path) == 0 {
		return fmt.Errorf("path cannot be empty")
	}

	// By default, use the uid and gid of the calling process.
	var uid = os.Getuid()
	var gid = os.Getgid()

	paths := getAllParentPaths(path)
	for _, curPath := range paths {
		info, err := os.Stat(curPath)

		if err != nil {
			if err = os.MkdirAll(curPath, perm); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			if err = syscall.Chown(curPath, uid, gid); err != nil {
				return fmt.Errorf("failed to change ownership: %w", err)
			}
			continue
		}

		if !info.IsDir() {
			return &os.PathError{Op: "mkdir", Path: curPath, Err: syscall.ENOTDIR}
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			uid = int(stat.Uid)
			gid = int(stat.Gid)
		} else {
			return fmt.Errorf("failed to retrieve UID and GID for path: %s", curPath)
		}
	}
	return nil
}

func RestoreStructFromJSON(file string) (any, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	var value any
	err = json.Unmarshal(content, &value)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	return value, nil
}

func SaveStructToJSON(file string, state any) error {
	structBytes, err := json.Marshal(state)
	if err != nil {
		log.Pretty("err: %v, state: %v", err, state)
		return fmt.Errorf("failed to serialize struct: %w", err)
	}
	return os.WriteFile(file, structBytes, defs.FileMode)
}

func SetReadonly(path string) error {
	// assume path is a valid direntry
	return filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0444)
		if info.IsDir() {
			mode = os.FileMode(0555)
		}
		return os.Chmod(path, mode)
	})
}

// remove state file in micran state directory
func RemoveExternalStatFile(id string) error {
	// if the file does not exist, return nil
	path := filepath.Join(defs.MicranStateDir, id, defs.MicantainerStateFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	log.Debugf("removing state file: %s", path)
	return os.Remove(path)
}

func RemoveStateDir(id string) error {
	return os.RemoveAll(filepath.Join(defs.MicranStateDir, id))
}
