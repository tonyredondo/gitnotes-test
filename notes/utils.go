package notes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

var errorMatcher = NewErrorMatcher()

type gitCommandHook func(args []string)

var (
	gitHookMu            sync.RWMutex
	beforeGitCommandHook gitCommandHook
	afterGitCommandHook  gitCommandHook
)

func runGitCommandHook(before bool, args []string) {
	gitHookMu.RLock()
	var hook gitCommandHook
	if before {
		hook = beforeGitCommandHook
	} else {
		hook = afterGitCommandHook
	}
	gitHookMu.RUnlock()
	if hook != nil {
		hook(args)
	}
}

func setGitCommandHooksForTesting(before, after gitCommandHook) func() {
	gitHookMu.Lock()
	prevBefore := beforeGitCommandHook
	prevAfter := afterGitCommandHook
	beforeGitCommandHook = before
	afterGitCommandHook = after
	gitHookMu.Unlock()

	return func() {
		gitHookMu.Lock()
		beforeGitCommandHook = prevBefore
		afterGitCommandHook = prevAfter
		gitHookMu.Unlock()
	}
}

// isSafeRevisionSpec performs light-weight validation to guard against obviously unsafe inputs.
// It intentionally allows typical git rev-specs (e.g., HEAD~1, branch names) and delegates
// existence checks to git itself.
func isSafeRevisionSpec(sha string) bool {
	if sha == "" {
		return true
	}

	if strings.HasPrefix(sha, "-") {
		return false
	}

	return !strings.ContainsRune(sha, '\x00')
}

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
	if !isSafeRevisionSpec(sha) {
		return &InvalidCommitShaError{CommitSha: sha}
	}

	if sha == "" {
		return nil
	}

	if _, _, err := executeGitCommand("rev-parse", "--verify", sha); err != nil {
		return &InvalidCommitShaError{CommitSha: sha}
	}

	return nil
}

// executeGitCommand is a helper function to run git commands and capture their output and errors.
// It returns stdout, stderr, and an error.
func executeGitCommand(args ...string) (string, string, error) {
	argsCopy := append([]string(nil), args...)
	runGitCommandHook(true, argsCopy)
	defer runGitCommandHook(false, argsCopy)

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
	argsCopy := append([]string(nil), args...)
	runGitCommandHook(true, argsCopy)
	defer runGitCommandHook(false, argsCopy)

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

// initializeEmptyNotesRef bootstraps an empty notes ref so it can be pushed to a remote.
// It creates an empty tree, commits it, and updates the ref to point to that commit.
func initializeEmptyNotesRef(ref string) error {
	emptyTreeSha, _, err := executeGitCommand("hash-object", "-t", "tree", "/dev/null")
	if err != nil {
		return fmt.Errorf("failed to compute empty tree for initializing %s: %w", ref, err)
	}

	commitSha, _, err := executeGitCommand("commit-tree", emptyTreeSha, "-m", fmt.Sprintf("Initialize %s", ref))
	if err != nil {
		return fmt.Errorf("failed to create initial notes commit for %s: %w", ref, err)
	}

	if _, _, err := executeGitCommand("update-ref", ref, strings.TrimSpace(commitSha)); err != nil {
		return fmt.Errorf("failed to update ref %s while initializing empty notes: %w", ref, err)
	}

	return nil
}

// notesRefExists checks whether a given notes ref exists locally.
// It returns (false, nil) when the ref is simply missing, and an error for other failures.
func notesRefExists(ref string) (bool, error) {
	args := []string{"show-ref", "--verify", "--quiet", ref}
	runGitCommandHook(true, args)
	defer runGitCommandHook(false, args)

	cmd := exec.Command("git", args...)
	cmd.Env = append(
		os.Environ(),
		"GIT_AUTHOR_NAME=Library Notes",
		"GIT_AUTHOR_EMAIL=lib@example.com",
		"GIT_COMMITTER_NAME=Library Notes",
		"GIT_COMMITTER_EMAIL=lib@example.com",
	)

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git show-ref failed while checking %s: %w", ref, err)
	}

	return true, nil
}
