//go:build windows

package workflow

// fsyncDir is a no-op on Windows, which does not support syncing a directory
// handle. The temp file's own Sync before the rename still provides the data
// durability guarantee.
func fsyncDir(string) {}
