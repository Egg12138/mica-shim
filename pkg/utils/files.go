package utils

import (
	"encoding/json"
	"fmt"
	"io"
	defs "mica-shim/definitions"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	cdtypes "github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/mount"

	log "mica-shim/logger"
)


func IsRegular(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return stat.Mode().IsRegular()
}

func IsFifo(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeNamedPipe != 0
}

func IsSymlink(path string) bool {
	stat, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeSymlink != 0
}

// return absolute and non-link path
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
		if os.IsNotExist(err) {
			return nil, err
		}
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
	log.Debugf("***SAVING %T TO %s", state, file)
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
	path := filepath.Join(defs.DefaultMicranStateDir, id, defs.MicantainerStateFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	log.Debugf("removing state file: %s", path)
	return os.Remove(path)
}

func RemoveStateDir(id string) error {
	return os.RemoveAll(filepath.Join(defs.DefaultMicranStateDir, id))
}

func MountDirs(mounts []*cdtypes.Mount, dest string) error {
	if len(mounts) == 0 {
		return nil
	}
	log.Debugf("mount to %s", dest)
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		if err := os.Mkdir(dest, 0711); err != nil {
			return err
		}
	}

	for _, rm := range mounts {
		m := &mount.Mount{
			Type:    rm.Type,
			Source:  rm.Source,
			Options: rm.Options,
		}
		if err := m.Mount(dest); err != nil {
			return fmt.Errorf("failed to mount: %v", m)
		}
	}

	
	return nil

}
func Backup(srcDir string) error {
	backupDir := "/tmp/backupbundle"
	
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}
	
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Debugf("Warning: skipping %s due to error: %v\n", path, err)
			return nil // Continue with other files
		}
		
		if !IsRegular(path) && !info.IsDir(){
			log.Debugf("Skipping %s\n", path)
			return nil
		}

		
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}
		
		destPath := filepath.Join(backupDir, relPath)
		
		log.Debugf("copy %s to %s", relPath, destPath)
		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}
		
		return copyFile(path, destPath, info.Mode())
	})
}

// copyFile copies a single file from src to dst with the given permissions
func copyFile(src, dst string, mode os.FileMode) error {
	// Create destination directory if needed
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}
	
	// Open source file
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", src, err)
	}
	defer srcFile.Close()
	
	// Create destination file
	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file %s: %w", dst, err)
	}
	defer dstFile.Close()
	
	// Copy file contents
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}
	
	// Set file permissions
	if err := os.Chmod(dst, mode); err != nil {
		return fmt.Errorf("failed to set file permissions: %w", err)
	}
	
	return nil
}

func TravelDir(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		depth := 0
		if relPath != "." {
			depth = len(strings.Split(relPath, string(os.PathSeparator)))
		}

		indent := strings.Repeat("  ", depth)
		log.Debugf("%s%s", indent, info.Name())

		return nil
	})
}
