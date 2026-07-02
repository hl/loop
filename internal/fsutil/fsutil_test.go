package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRegularFileRejectsSymlink(t *testing.T) {
	t.Chdir(t.TempDir())

	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, "link"); err != nil {
		t.Skip("symlinks not supported")
	}

	_, err := OpenRegularFile("link")
	if err == nil {
		t.Error("expected error when opening symlink")
	}
}

func TestOpenRegularFileRejectsDirectory(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := os.Mkdir("dir", 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := OpenRegularFile("dir")
	if err == nil {
		t.Error("expected error when opening directory")
	}
}

func TestOpenRegularFileSuccess(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := os.WriteFile("regular.txt", []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := OpenRegularFile("regular.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = f.Close()
}

func TestReadRegularFile(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := os.WriteFile("data.txt", []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := ReadRegularFile("data.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("expected 'content', got %q", string(data))
	}
}

func TestReadRegularFileRejectsSymlink(t *testing.T) {
	t.Chdir(t.TempDir())

	target := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, "link.txt"); err != nil {
		t.Skip("symlinks not supported")
	}

	_, err := ReadRegularFile("link.txt")
	if err == nil {
		t.Error("expected error when reading symlink")
	}
}

func TestReadRegularFileCappedUnderLimit(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := os.WriteFile("data.txt", []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := ReadRegularFileCapped("data.txt", 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %q", string(data))
	}
}

func TestReadRegularFileCappedAtLimit(t *testing.T) {
	t.Chdir(t.TempDir())

	// A file of exactly the cap is accepted (inclusive limit).
	if err := os.WriteFile("data.txt", []byte("abcde"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRegularFileCapped("data.txt", 5); err != nil {
		t.Errorf("expected file of exactly the cap to be accepted, got: %v", err)
	}
}

func TestReadRegularFileCappedOverLimit(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := os.WriteFile("big.txt", []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ReadRegularFileCapped("big.txt", 4)
	if err == nil {
		t.Fatal("expected error when file exceeds cap")
	}
	if !strings.Contains(err.Error(), "big.txt") || !strings.Contains(err.Error(), "4") {
		t.Errorf("expected error to name the file and limit, got: %v", err)
	}
}

func TestReadRegularFileCappedRejectsSymlink(t *testing.T) {
	t.Chdir(t.TempDir())

	target := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, "link.txt"); err != nil {
		t.Skip("symlinks not supported")
	}

	if _, err := ReadRegularFileCapped("link.txt", 1024); err == nil {
		t.Error("expected error when reading symlink")
	}
}

func TestIsRegularFile(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := os.WriteFile("file.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsRegularFile("file.txt") {
		t.Error("expected true for regular file")
	}
	if IsRegularFile("nonexistent") {
		t.Error("expected false for nonexistent file")
	}
	if err := os.Mkdir("dir", 0o755); err != nil {
		t.Fatal(err)
	}
	if IsRegularFile("dir") {
		t.Error("expected false for directory")
	}
}
