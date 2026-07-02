package workflow

import (
	"fmt"
	"os"
	"path/filepath"
)

func atomicWriteRegularFile(path string, data []byte, perm os.FileMode) error {
	if err := rejectNonRegularPath(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".brr-state-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	// Flush the data to stable storage before the rename. Without this, a power
	// loss can make the rename durable ahead of the data blocks, leaving a
	// zero-byte state file that fails to parse (the workflow then "starts fresh"
	// and repeats already-completed stages).
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := rejectNonRegularPath(path); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	// Persist the directory entry so the rename itself survives a crash (no-op on
	// platforms that do not support directory fsync).
	fsyncDir(dir)
	return nil
}

func appendRegularFile(path string, data []byte, perm os.FileMode) error {
	fi, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if os.IsNotExist(err) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		_, err = f.Write(data)
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	fi2, err := f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(fi, fi2) {
		return fmt.Errorf("%s: file changed between stat and open", path)
	}
	_, err = f.Write(data)
	return err
}

func ensureStateDir() error {
	for _, path := range []string{".brr", filepath.Join(".brr", "state"), StateDir} {
		if err := rejectUnsafeDirectory(path); err != nil {
			return err
		}
	}
	return os.MkdirAll(StateDir, 0o755)
}

func ensureWorkflowDir() error {
	for _, path := range []string{".brr", filepath.Join(".brr", "workflows")} {
		if err := rejectUnsafeDirectory(path); err != nil {
			return err
		}
	}
	return os.MkdirAll(filepath.Join(".brr", "workflows"), 0o755)
}

func workflowPath(name string) string {
	return filepath.Join(".brr", "workflows", name+".yaml")
}

func rejectUnsafeDirectory(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("checking %s: %w", path, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink", path)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}

func rejectUnsafeExisting(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("checking %s: %w", path, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink", path)
	}
	return nil
}

func rejectNonRegularPath(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("checking %s: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}
