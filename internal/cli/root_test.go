package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePromptInlineText(t *testing.T) {
	t.Chdir(t.TempDir())

	text, err := resolvePrompt("Fix all the bugs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "Fix all the bugs" {
		t.Errorf("expected inline text, got %q", text)
	}
}

func TestResolvePromptFromFile(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := os.WriteFile("my-prompt.md", []byte("do the thing"), 0o644); err != nil {
		t.Fatal(err)
	}

	text, err := resolvePrompt("my-prompt.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "do the thing" {
		t.Errorf("expected file content, got %q", text)
	}
}

func TestResolvePromptDirectoryFallsThrough(t *testing.T) {
	t.Chdir(t.TempDir())

	// Create a directory named "task" AND a named prompt .brr/prompts/task.md
	if err := os.Mkdir("task", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(".brr", "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".brr", "prompts", "task.md"), []byte("named prompt"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Should skip the directory and resolve the named prompt
	text, err := resolvePrompt("task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "named prompt" {
		t.Errorf("expected named prompt, got %q", text)
	}
}

func TestResolvePromptNamedFromProject(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := os.MkdirAll(filepath.Join(".brr", "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".brr", "prompts", "task.md"), []byte("task prompt"), 0o644); err != nil {
		t.Fatal(err)
	}

	text, err := resolvePrompt("task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "task prompt" {
		t.Errorf("expected named prompt content, got %q", text)
	}
}

func TestResolvePromptEmptyNamedPromptRejected(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := os.MkdirAll(filepath.Join(".brr", "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".brr", "prompts", "build.md"), []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Empty resolved prompts must be rejected in resolvePrompt itself so workflow
	// stages (run and validate) get the same guard as `brr run`.
	_, err := resolvePrompt("build")
	if err == nil {
		t.Fatal("expected empty named prompt to be rejected")
	}
	if !strings.Contains(err.Error(), "prompt is empty") {
		t.Fatalf("expected 'prompt is empty' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), filepath.Join(".brr", "prompts", "build.md")) {
		t.Fatalf("expected error to name the resolved source, got: %v", err)
	}
}

func TestResolvePromptEmptyInlineRejected(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := resolvePrompt("   ")
	if err == nil {
		t.Fatal("expected whitespace-only inline prompt to be rejected")
	}
	if !strings.Contains(err.Error(), "prompt is empty") {
		t.Fatalf("expected 'prompt is empty' error, got: %v", err)
	}
}

func TestResolvePromptMissingFile(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := resolvePrompt("nonexistent.md")
	if err == nil {
		t.Error("expected error for missing file with .md extension")
	}
}

func TestResolvePromptPathTraversal(t *testing.T) {
	t.Chdir(t.TempDir())

	// This exercises the looksLikeFilePath path-separator check (file not found)
	_, err := resolvePrompt("../../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal attempt")
	}
}

func TestResolvePromptNamedPathTraversalGuard(t *testing.T) {
	t.Chdir(t.TempDir())

	// This exercises the explicit ".." rejection in the named prompt branch.
	// Use a bare name without spaces/separators so it enters named prompt resolution.
	_, err := resolvePrompt("..secret")
	if err == nil {
		t.Error("expected error for named prompt with '..' in name")
	}
	if err != nil && !strings.Contains(err.Error(), "invalid prompt name") {
		t.Errorf("expected 'invalid prompt name' error, got: %v", err)
	}
}

func TestResolvePromptDottedInlineText(t *testing.T) {
	t.Chdir(t.TempDir())

	// A bare name with dots but no recognized extension should be tried as a named prompt
	// and then fall through to inline text (not error as "file not found")
	text, err := resolvePrompt("v1.2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "v1.2" {
		t.Errorf("expected inline text, got %q", text)
	}
}

func TestResolvePromptInlineTextWithSlash(t *testing.T) {
	t.Chdir(t.TempDir())

	// Inline prompts containing slashes should work (README example)
	text, err := resolvePrompt("Fix all TODO comments in src/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "Fix all TODO comments in src/" {
		t.Errorf("expected inline text, got %q", text)
	}
}

func TestResolvePromptInlineTextWithPathInMiddle(t *testing.T) {
	t.Chdir(t.TempDir())

	text, err := resolvePrompt("Refactor pkg/config/loader.go to use generics")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "Refactor pkg/config/loader.go to use generics" {
		t.Errorf("expected inline text, got %q", text)
	}
}

func TestLooksLikeFilePathWithSeparatorNoSpaces(t *testing.T) {
	// A path-like string with separators is treated as a file path
	if !looksLikeFilePath("path/to/file") {
		t.Error("expected true for path with separators")
	}
}

func TestLooksLikeFilePathWithSeparatorAndExtension(t *testing.T) {
	// path/to/file.md without spaces IS a file path
	if !looksLikeFilePath("path/to/file.md") {
		t.Error("expected true for path with .md extension")
	}
}

func TestLooksLikeFilePathWithSpaces(t *testing.T) {
	// Text with spaces and slashes but no prompt extension → inline text
	if looksLikeFilePath("Fix stuff in src/") {
		t.Error("expected false for text with spaces and no prompt extension")
	}
}

func TestLooksLikeFilePathWithSpacesAndExtension(t *testing.T) {
	// Path with spaces AND separator AND prompt extension → file path
	if !looksLikeFilePath("docs/Build Plan.md") {
		t.Error("expected true for path with spaces, separator, and .md extension")
	}
	// Extension without separator → inline text (e.g. "fix login.md module")
	if looksLikeFilePath("fix login.md") {
		t.Error("expected false for text with spaces and extension but no separator")
	}
}

func TestLooksLikeFilePathWithExtension(t *testing.T) {
	if !looksLikeFilePath("prompt.md") {
		t.Error("expected true for .md extension")
	}
	if !looksLikeFilePath("notes.txt") {
		t.Error("expected true for .txt extension")
	}
}

func TestLooksLikeFilePathPlainName(t *testing.T) {
	if looksLikeFilePath("task") {
		t.Error("expected false for plain name without extension")
	}
}

func TestLooksLikeFilePathDottedName(t *testing.T) {
	// Dotted names like "v1.2" should NOT look like file paths
	if looksLikeFilePath("v1.2") {
		t.Error("expected false for dotted name without recognized extension")
	}
}

func TestResolvePromptNamedSymlinkRejected(t *testing.T) {
	t.Chdir(t.TempDir())

	// Create a symlink at .brr/prompts/evil.md -> /etc/hosts
	if err := os.MkdirAll(filepath.Join(".brr", "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(target, []byte("secret data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(".brr", "prompts", "evil.md")); err != nil {
		t.Skip("symlinks not supported")
	}

	// Should NOT read through the symlink or silently treat the prompt name as
	// inline text. A named prompt path exists, but it is unsafe.
	_, err := resolvePrompt("evil")
	if err == nil {
		t.Fatal("expected symlinked named prompt to be rejected")
	}
	if !strings.Contains(err.Error(), filepath.Join(".brr", "prompts", "evil.md")) {
		t.Errorf("expected error to mention named prompt path, got: %v", err)
	}
}

func TestResolvePromptDirectFileSymlinkRejected(t *testing.T) {
	t.Chdir(t.TempDir())

	// Create a symlink to a regular file — direct path resolution should reject it
	target := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, "prompt-link.md"); err != nil {
		t.Skip("symlinks not supported")
	}

	// Should not read through the symlink or treat it as inline prompt text.
	_, err := resolvePrompt("prompt-link.md")
	if err == nil {
		t.Fatal("expected symlink prompt file to be rejected")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("expected regular-file error, got: %v", err)
	}
}

func TestResolvePromptNamedFileTooLarge(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := os.MkdirAll(filepath.Join(".brr", "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	large := bytes.Repeat([]byte("x"), maxPromptFileSize+1)
	if err := os.WriteFile(filepath.Join(".brr", "prompts", "huge.md"), large, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := resolvePrompt("huge")
	if err == nil {
		t.Fatal("expected oversized named prompt to be rejected")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected size error, got: %v", err)
	}
}

func TestResolveWorkflowPromptRejectsAbsolutePath(t *testing.T) {
	t.Chdir(t.TempDir())
	secret := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(secret, []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A workflow prompt must not read an absolute out-of-tree file.
	_, err := resolveWorkflowPrompt(secret)
	if err == nil {
		t.Fatal("expected absolute workflow prompt path to be rejected")
	}
	if strings.Contains(err.Error(), "top secret") {
		t.Fatalf("error must not leak file contents: %v", err)
	}
	if !strings.Contains(err.Error(), "working tree") {
		t.Fatalf("expected working-tree restriction error, got: %v", err)
	}
}

func TestResolveWorkflowPromptRejectsTraversal(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := resolveWorkflowPrompt("../../etc/passwd")
	if err == nil {
		t.Fatal("expected .. traversal to be rejected")
	}
	if !strings.Contains(err.Error(), "working tree") {
		t.Fatalf("expected working-tree restriction error, got: %v", err)
	}
}

func TestResolveWorkflowPromptAllowsNamedRelativeAndInline(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join(".brr", "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".brr", "prompts", "build.md"), []byte("named"), 0o644); err != nil {
		t.Fatal(err)
	}
	if text, err := resolveWorkflowPrompt("build"); err != nil || text != "named" {
		t.Fatalf("named prompt: text=%q err=%v", text, err)
	}
	if err := os.WriteFile("local.md", []byte("relative"), 0o644); err != nil {
		t.Fatal(err)
	}
	if text, err := resolveWorkflowPrompt("local.md"); err != nil || text != "relative" {
		t.Fatalf("relative in-tree file: text=%q err=%v", text, err)
	}
	// Inline text (including punctuation like "..") must still pass through.
	if text, err := resolveWorkflowPrompt("Review the diff... carefully"); err != nil || text != "Review the diff... carefully" {
		t.Fatalf("inline: text=%q err=%v", text, err)
	}
}

func TestResolvePromptRootStillReadsAbsolute(t *testing.T) {
	t.Chdir(t.TempDir())
	f := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(f, []byte("do it"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The root CLI arg is typed by the user, so absolute paths stay permissive.
	text, err := resolvePrompt(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "do it" {
		t.Fatalf("expected file content, got %q", text)
	}
}

func TestResolvePromptPathWithSeparator(t *testing.T) {
	t.Chdir(t.TempDir())

	// A path-like string with separators but no recognized extension
	// should error, not become inline text
	_, err := resolvePrompt("prompts/task")
	if err == nil {
		t.Error("expected error for path-like argument without extension")
	}
}
