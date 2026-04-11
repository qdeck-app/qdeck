package git

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/qdeck-app/qdeck/infrastructure/executil"
)

// RepoRoot returns the git repository root for the directory containing filePath.
// Returns an error if the path is not inside a git repository.
func RepoRoot(ctx context.Context, filePath string) (string, error) {
	dir := filepath.Dir(filePath)

	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel") //nolint:gosec // args are not user-controlled
	executil.HideWindow(cmd)

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse in %s: %w", dir, err)
	}

	return strings.TrimSpace(string(out)), nil
}

// ShowHEAD returns the contents of filePath at the HEAD revision.
// Returns an error if git is not available, the file is untracked, or has never been committed.
func ShowHEAD(ctx context.Context, filePath string) ([]byte, error) {
	root, err := RepoRoot(ctx, filePath)
	if err != nil {
		return nil, err
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute path %s: %w", filePath, err)
	}

	relPath, err := filepath.Rel(root, absPath)
	if err != nil {
		return nil, fmt.Errorf("compute relative path from %s to %s: %w", root, absPath, err)
	}

	// git show expects forward slashes even on Windows.
	relPath = filepath.ToSlash(relPath)

	cmd := exec.CommandContext(ctx, "git", "-C", root, "show", "HEAD:"+relPath) //nolint:gosec // args are not user-controlled
	executil.HideWindow(cmd)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git show HEAD:%s: %w", relPath, err)
	}

	return out, nil
}
