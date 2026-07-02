package fsutil

import (
	"fmt"
	"io"
	"os"
)

// ErrNotRegularFile is returned when a path exists but is not a regular file.
var ErrNotRegularFile = fmt.Errorf("not a regular file")

// OpenRegularFile opens path only if it is a regular file (not a symlink, dir, etc.).
// Uses Lstat→Open→Fstat(SameFile) to prevent TOCTOU symlink-swap attacks.
func OpenRegularFile(path string) (*os.File, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, ErrNotRegularFile
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fi2, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !os.SameFile(fi, fi2) {
		_ = f.Close()
		return nil, fmt.Errorf("%s: file changed between stat and open", path)
	}
	return f, nil
}

// ReadRegularFile reads path only if it is a regular file (not a symlink, dir, etc.).
func ReadRegularFile(path string) ([]byte, error) {
	f, err := OpenRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

// ReadRegularFileCapped reads path only if it is a regular file, reading at most
// maxBytes. It returns an error naming the path and limit if the file exceeds
// maxBytes, so a planted oversized file cannot exhaust memory. maxBytes is the
// inclusive limit: a file of exactly maxBytes is accepted.
func ReadRegularFileCapped(path string, maxBytes int64) ([]byte, error) {
	f, err := OpenRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%s exceeds maximum size of %d bytes", path, maxBytes)
	}
	return data, nil
}

// IsRegularFile returns true if path exists and is a regular file.
func IsRegularFile(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return fi.Mode().IsRegular()
}
