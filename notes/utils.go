package notes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var errorMatcher = NewErrorMatcher()

// formatNamespaceRef ensures the namespace has the correct prefix for git.
// If the namespace already starts with "refs/notes/", it's returned as is.
// Otherwise, "refs/notes/" is prepended.
// If the namespace is empty, it uses git's default notes ref "refs/notes/commits".
func formatNamespaceRef(namespace string) string {
	if namespace == "" {
		return "refs/notes/commits" // Default git notes ref
	}
	if strings.HasPrefix(namespace, "refs/notes/") {
		return namespace
	}
	return "refs/notes/" + namespace
}

// validateCommitSHA validates that a commit SHA is in the correct format.
// It allows empty strings (which will be resolved to HEAD), but checks for
// potentially dangerous inputs and validates hex format.
func validateCommitSHA(sha string) error {
	if !errorMatcher.ValidateCommitSHA(sha) {
		return &InvalidCommitShaError{CommitSha: sha}
	}
	return nil
}

// executeGitCommand is a helper function to run git commands and capture their output and errors.
// It returns stdout, stderr, and an error.
func executeGitCommand(args ...string) (string, string, error) {
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = append(
		os.Environ(),
		"GIT_AUTHOR_NAME=Library Notes",
		"GIT_AUTHOR_EMAIL=lib@example.com",
		"GIT_COMMITTER_NAME=Library Notes",
		"GIT_COMMITTER_EMAIL=lib@example.com",
	)

	err := cmd.Run()

	if err != nil {
		// Check for specific exit codes
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.String(), stderr.String(), fmt.Errorf("git %s failed with exit code %d: %w; stderr: %s",
				args[0], exitErr.ExitCode(), err, stderr.String())
		}
		return stdout.String(), stderr.String(), fmt.Errorf("git %s failed: %w; stderr: %s", args[0], err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), nil
}

// executeGitCommandContext is like executeGitCommand but with context support for cancellation
func executeGitCommandContext(ctx context.Context, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = append(
		os.Environ(),
		"GIT_AUTHOR_NAME=Library Notes",
		"GIT_AUTHOR_EMAIL=lib@example.com",
		"GIT_COMMITTER_NAME=Library Notes",
		"GIT_COMMITTER_EMAIL=lib@example.com",
	)

	err := cmd.Run()

	if err != nil {
		// Check if context was cancelled
		if ctx.Err() != nil {
			return stdout.String(), stderr.String(), fmt.Errorf("command cancelled: %w", ctx.Err())
		}

		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.String(), stderr.String(), fmt.Errorf("git %s failed with exit code %d: %w; stderr: %s",
				args[0], exitErr.ExitCode(), err, stderr.String())
		}
		return stdout.String(), stderr.String(), fmt.Errorf("git %s failed: %w; stderr: %s", args[0], err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), nil
}
