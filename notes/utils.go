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
	"time"
)

var errorMatcher = NewErrorMatcher()

// GitCommandError captures structured failure details for git command execution.
type GitCommandError struct {
	Command  string
	Args     []string
	ExitCode int
	Stdout   string
	Stderr   string
	Cause    error
}

func (e *GitCommandError) Error() string {
	return fmt.Sprintf("git %s failed with exit code %d: %v; stderr: %s", e.Command, e.ExitCode, e.Cause, e.Stderr)
}

func (e *GitCommandError) Unwrap() error {
	return e.Cause
}

func extractGitCommandError(err error) *GitCommandError {
	var gitErr *GitCommandError
	if errors.As(err, &gitErr) {
		return gitErr
	}
	return nil
}

type gitCommandHook func(args []string)
type gitCommandMetricsHook interface {
	OnCommandStart(args []string)
	OnCommandEnd(args []string, duration time.Duration, err error)
}

var (
	gitHookMu            sync.RWMutex
	beforeGitCommandHook gitCommandHook
	afterGitCommandHook  gitCommandHook
	commandMetricsHook   gitCommandMetricsHook

	gitEnvOnce sync.Once
	baseGitEnv []string
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

func setGitCommandMetricsHookForTesting(hook gitCommandMetricsHook) func() {
	gitHookMu.Lock()
	prev := commandMetricsHook
	commandMetricsHook = hook
	gitHookMu.Unlock()

	return func() {
		gitHookMu.Lock()
		commandMetricsHook = prev
		gitHookMu.Unlock()
	}
}

func runCommandMetricsStart(args []string) {
	gitHookMu.RLock()
	hook := commandMetricsHook
	gitHookMu.RUnlock()
	if hook != nil {
		hook.OnCommandStart(args)
	}
}

func runCommandMetricsEnd(args []string, duration time.Duration, err error) {
	gitHookMu.RLock()
	hook := commandMetricsHook
	gitHookMu.RUnlock()
	if hook != nil {
		hook.OnCommandEnd(args, duration, err)
	}
}

func getBaseGitEnv() []string {
	gitEnvOnce.Do(func() {
		baseGitEnv = append(
			os.Environ(),
			"GIT_AUTHOR_NAME=Library Notes",
			"GIT_AUTHOR_EMAIL=lib@example.com",
			"GIT_COMMITTER_NAME=Library Notes",
			"GIT_COMMITTER_EMAIL=lib@example.com",
		)
	})
	return baseGitEnv
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
	if !errorMatcher.ValidateCommitSHA(sha) {
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
	runCommandMetricsStart(argsCopy)
	start := time.Now()

	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = getBaseGitEnv()

	err := cmd.Run()
	duration := time.Since(start)
	runCommandMetricsEnd(argsCopy, duration, err)

	if err != nil {
		// Check for specific exit codes
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.String(), stderr.String(), &GitCommandError{
				Command:  args[0],
				Args:     append([]string(nil), args...),
				ExitCode: exitErr.ExitCode(),
				Stdout:   strings.TrimSpace(stdout.String()),
				Stderr:   strings.TrimSpace(stderr.String()),
				Cause:    err,
			}
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
	runCommandMetricsStart(argsCopy)
	start := time.Now()

	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = getBaseGitEnv()

	err := cmd.Run()
	duration := time.Since(start)
	runCommandMetricsEnd(argsCopy, duration, err)

	if err != nil {
		// Check if context was cancelled
		if ctx.Err() != nil {
			return stdout.String(), stderr.String(), fmt.Errorf("command cancelled: %w", ctx.Err())
		}

		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.String(), stderr.String(), &GitCommandError{
				Command:  args[0],
				Args:     append([]string(nil), args...),
				ExitCode: exitErr.ExitCode(),
				Stdout:   strings.TrimSpace(stdout.String()),
				Stderr:   strings.TrimSpace(stderr.String()),
				Cause:    err,
			}
		}
		return stdout.String(), stderr.String(), fmt.Errorf("git %s failed: %w; stderr: %s", args[0], err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), nil
}

// executeGitCommandWithStdin executes a git command and writes stdin before waiting for output.
func executeGitCommandWithStdin(stdin string, args ...string) (string, string, error) {
	argsCopy := append([]string(nil), args...)
	runGitCommandHook(true, argsCopy)
	defer runGitCommandHook(false, argsCopy)
	runCommandMetricsStart(argsCopy)
	start := time.Now()

	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = getBaseGitEnv()

	err := cmd.Run()
	duration := time.Since(start)
	runCommandMetricsEnd(argsCopy, duration, err)

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.String(), stderr.String(), &GitCommandError{
				Command:  args[0],
				Args:     append([]string(nil), args...),
				ExitCode: exitErr.ExitCode(),
				Stdout:   strings.TrimSpace(stdout.String()),
				Stderr:   strings.TrimSpace(stderr.String()),
				Cause:    err,
			}
		}
		return stdout.String(), stderr.String(), fmt.Errorf("git %s failed: %w; stderr: %s", args[0], err, stderr.String())
	}
	return stdout.String(), strings.TrimSpace(stderr.String()), nil
}
