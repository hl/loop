//go:build !windows

package workflow

import "os"

// fsyncDir flushes a directory's metadata so a rename into it survives a crash.
// Best-effort: any error is ignored (the data file itself is already synced).
func fsyncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	defer func() { _ = d.Close() }()
	_ = d.Sync()
}
