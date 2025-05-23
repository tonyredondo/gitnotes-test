package notes

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// MaxNoteSize is the maximum allowed size for a single note (10MB)
	MaxNoteSize = 10 * 1024 * 1024
	// MaxJSONObjects is the maximum number of JSON objects allowed in a single note
	MaxJSONObjects = 1000
	// DefaultRetryAttempts is the default number of retry attempts for push operations
	DefaultRetryAttempts = 3
)

// GetNote retrieves the content of a note for a specific commit SHA in a namespace.
func GetNote(namespace, commitSha string) (string, error) {
	return GetNoteWithContext(context.Background(), namespace, commitSha)
}

// GetNoteWithContext retrieves the content of a note with context support for cancellation
func GetNoteWithContext(ctx context.Context, namespace, commitSha string) (string, error) {
	if err := validateCommitSHA(commitSha); err != nil {
		return "", err
	}

	ref := formatNamespaceRef(namespace)

	if commitSha == "" {
		var err error
		commitSha, _, err = executeGitCommandContext(ctx, "rev-parse", "HEAD")
		if err != nil {
			return "", fmt.Errorf("failed to resolve HEAD: %w", err)
		}
	}

	stdout, stderr, err := executeGitCommandContext(ctx, "notes", "--ref", ref, "show", commitSha)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		errStr := err.Error()
		stderrLower := strings.ToLower(stderr)

		// Check exit code first
		if strings.Contains(errStr, "exit code 1") &&
			(strings.Contains(stderrLower, "no note found") ||
				strings.Contains(stderrLower, "no notes found")) {
			return "", &NoteNotFoundError{Namespace: namespace, CommitSha: commitSha}
		}

		if strings.Contains(errStr, "no note found for object") ||
			strings.Contains(errStr, "failed to get note") {
			return "", &NoteNotFoundError{Namespace: namespace, CommitSha: commitSha}
		}
		if strings.Contains(errStr, "fatal: failed to resolve") ||
			strings.Contains(errStr, "exit code 128") {
			return "", &InvalidCommitShaError{CommitSha: commitSha}
		}
		return "", fmt.Errorf("failed to get note for %s in %s: %w", commitSha, ref, err)
	}
	return stdout, nil
}

// GetNotesBulk retrieves notes for multiple commit SHAs in parallel
func GetNotesBulk(namespace string, commitShas []string) (map[string]string, map[string]error) {
	results := make(map[string]string)
	errors := make(map[string]error)

	// Validate all SHAs first
	for _, sha := range commitShas {
		if err := validateCommitSHA(sha); err != nil {
			errors[sha] = err
		}
	}

	// Use goroutines with semaphore for parallel fetching
	sem := make(chan struct{}, 10) // Limit concurrent operations
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, sha := range commitShas {
		if _, hasError := errors[sha]; hasError {
			continue // Skip invalid SHAs
		}

		wg.Add(1)
		go func(commitSha string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			note, err := GetNote(namespace, commitSha)
			mu.Lock()
			if err != nil {
				errors[commitSha] = err
			} else {
				results[commitSha] = note
			}
			mu.Unlock()
		}(sha)
	}

	wg.Wait()
	return results, errors
}

// SetNote sets (or overwrites) a note for a specific commit SHA in a namespace.
func SetNote(namespace, commitSha, value string) error {
	if err := validateCommitSHA(commitSha); err != nil {
		return err
	}

	if len(value) > MaxNoteSize {
		return &NoteSizeExceededError{Size: len(value), MaxSize: MaxNoteSize}
	}

	ref := formatNamespaceRef(namespace)

	if commitSha == "" {
		var err error
		commitSha, _, err = executeGitCommand("rev-parse", "HEAD")
		if err != nil {
			return fmt.Errorf("failed to resolve HEAD: %w", err)
		}
	}

	stdout, stderr, err := executeGitCommand("notes", "--ref", ref, "add", "-f", "-m", value, commitSha)
	if err != nil {
		return fmt.Errorf("failed to set note for %s in %s (stdout: %s | stderr: %s): %w", commitSha, ref, stdout, stderr, err)
	}
	return nil
}

// Helper struct to hold commit SHA and its timestamp
type commitInfo struct {
	Sha       string
	Timestamp int64
}

// GetNoteList retrieves a list of commit SHAs that have notes in a given namespace,
// sorted in reverse chronological order (newest first).
func GetNoteList(namespace string) ([]string, error) {
	ref := formatNamespaceRef(namespace)

	listOutput, _, err := executeGitCommand("notes", "--ref", ref, "list")
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "bad notes ref") || strings.Contains(errMsg, "does not exist") ||
			strings.Contains(errMsg, "no notes found") || strings.Contains(errMsg, "exit code 1") {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to list notes in %s: %w", ref, err)
	}

	if listOutput == "" {
		return []string{}, nil
	}

	// Collect all commit SHAs
	var commitShas []string
	shaToNoteObj := make(map[string]string)

	scanner := bufio.NewScanner(strings.NewReader(listOutput))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			noteObj := parts[0]
			commitSha := parts[1]
			commitShas = append(commitShas, commitSha)
			shaToNoteObj[commitSha] = noteObj
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning 'git notes list' output for %s: %w", ref, err)
	}

	if len(commitShas) == 0 {
		return []string{}, nil
	}

	// Get all timestamps in one batch call
	args := append([]string{"show", "-s", "--format=%H %ct"}, commitShas...)
	timestampOutput, _, err := executeGitCommand(args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get timestamps for commits: %w", err)
	}

	// Parse timestamps
	var commitsWithNotes []commitInfo
	timestampScanner := bufio.NewScanner(strings.NewReader(timestampOutput))
	for timestampScanner.Scan() {
		line := timestampScanner.Text()
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			sha := parts[0]
			timestamp, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse timestamp for commit %s: %w", sha, err)
			}
			commitsWithNotes = append(commitsWithNotes, commitInfo{Sha: sha, Timestamp: timestamp})
		}
	}

	// Sort by timestamp in descending order (newest first)
	sort.Slice(commitsWithNotes, func(i, j int) bool {
		return commitsWithNotes[i].Timestamp > commitsWithNotes[j].Timestamp
	})

	// Extract just the SHAs
	sortedShas := make([]string, len(commitsWithNotes))
	for i, ci := range commitsWithNotes {
		sortedShas[i] = ci.Sha
	}

	return sortedShas, nil
}

// DeleteNote removes a note for a specific commit SHA in a namespace.
func DeleteNote(namespace, commitSha string) error {
	if commitSha == "" {
		return fmt.Errorf("commitSha cannot be empty")
	}

	if err := validateCommitSHA(commitSha); err != nil {
		return err
	}

	ref := formatNamespaceRef(namespace)
	_, stderr, err := executeGitCommand("notes", "--ref", ref, "remove", commitSha)
	if err != nil {
		// Check if the note doesn't exist (not an error in delete context)
		if strings.Contains(stderr, "object has no note") ||
			strings.Contains(err.Error(), "exit code 1") {
			return nil // Idempotent delete
		}
		return fmt.Errorf("failed to delete note for %s in %s (stderr: %s): %w", commitSha, ref, stderr, err)
	}
	return nil
}

// FetchNotes fetches notes from a remote for a specific namespace and attempts to update the local notes ref.
// It uses `git fetch --force <remoteName> <refSpec>:<refSpec>` to overwrite local changes if divergence occurs.
func FetchNotes(namespace, remoteName string) error {
	if remoteName == "" {
		return fmt.Errorf("remoteName cannot be empty")
	}
	localRef := formatNamespaceRef(namespace)
	// The refspec fetches the remote notes ref and updates the local one with the same name.
	// e.g., refs/notes/mynamespace:refs/notes/mynamespace
	fullRefSpec := fmt.Sprintf("%s:%s", localRef, localRef)

	// 1. Fetch the notes reference itself
	_, stderrOutput, err := executeGitCommand("fetch", "--force", remoteName, fullRefSpec)
	if err != nil {
		// Check if the error is because the remote ref doesn't exist
		stderrLower := strings.ToLower(stderrOutput)
		if strings.Contains(stderrLower, "couldn't find remote ref") ||
			strings.Contains(stderrLower, "no such ref") ||
			strings.Contains(stderrLower, "fetch-pack: invalid refspec") ||
			strings.Contains(err.Error(), "exit status 1") ||
			strings.Contains(err.Error(), "exit status 128") {
			// Remote doesn't have this notes ref yet, not an error
			return nil
		}
		return fmt.Errorf("failed to fetch notes for namespace %s (refspec %s) from %s (stderr: %s): %w",
			namespace, fullRefSpec, remoteName, stderrOutput, err)
	}

	// 2. After fetching notes, list all commits referenced by these notes.
	// The original code proceeds even if listing notes fails or returns empty,
	// so we'll maintain that behavior for this part.
	listOutput, _, listErr := executeGitCommand("notes", "--ref", localRef, "list")

	// Only proceed if listing notes was successful and produced output.
	if listErr == nil && listOutput != "" {
		scanner := bufio.NewScanner(strings.NewReader(listOutput))
		// Using a map to store commitShas ensures uniqueness efficiently.
		commitShasToFetch := make(map[string]struct{})

		for scanner.Scan() {
			parts := strings.Fields(scanner.Text())
			// Output of `git notes list` is typically "<note-object-sha> <commit-sha>"
			if len(parts) >= 2 {
				commitSha := parts[1]
				commitShasToFetch[commitSha] = struct{}{}
			}
		}

		// 3. If there are any commit SHAs referenced by the notes, fetch them all in a single command.
		if len(commitShasToFetch) > 0 {
			// Convert map keys to a slice of SHAs for the command arguments.
			shas := make([]string, 0, len(commitShasToFetch))
			for sha := range commitShasToFetch {
				shas = append(shas, sha)
			}

			// Prepare arguments for `git fetch <remoteName> <sha1> <sha2> ...`
			fetchArgs := []string{"fetch", remoteName}
			fetchArgs = append(fetchArgs, shas...)
			_, _, _ = executeGitCommand(fetchArgs...)
		}
	}
	// If listErr was not nil or listOutput was empty, the block above is skipped.
	// The function returns nil, indicating success for the primary operation of fetching the notes ref,
	// consistent with the original function's behavior.
	return nil
}

// PushNotes fetches remote notes for the given namespace, merges them into the local notes
// using the 'cat_sort_uniq' strategy, and then pushes the combined result to the remote.
func PushNotes(namespace, remoteName string) error {
	return PushNotesWithRetry(namespace, remoteName, DefaultRetryAttempts)
}

// PushNotesWithRetry is like PushNotes but with configurable retry attempts
func PushNotesWithRetry(namespace, remoteName string, maxRetries int) error {
	if remoteName == "" {
		return fmt.Errorf("remoteName cannot be empty")
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		err := pushNotesAttempt(namespace, remoteName)
		if err == nil {
			return nil
		}

		// Check if error is due to non-fast-forward (concurrent modification)
		if strings.Contains(err.Error(), "non-fast-forward") ||
			strings.Contains(err.Error(), "fetch first") ||
			strings.Contains(err.Error(), "rejected") {
			if attempt < maxRetries-1 {
				// log.Printf("Push failed due to concurrent modification, retry %d/%d", attempt+1, maxRetries)
				// Exponential backoff
				time.Sleep(time.Duration(attempt*attempt) * 100 * time.Millisecond)
				continue
			}
		}

		// Non-retryable error or last attempt
		return err
	}

	return fmt.Errorf("push failed after %d attempts", maxRetries)
}

// pushNotesAttempt performs a single attempt to push notes
func pushNotesAttempt(namespace, remoteName string) error {
	localRef := formatNamespaceRef(namespace) // e.g., refs/notes/my_namespace

	// First, ensure we're in a clean state (abort any previous merge)
	// This is safe to run even if there's no merge in progress
	_, _, _ = executeGitCommand("notes", "--ref", localRef, "merge", "--abort")

	// 1. Fetch remote notes. This updates the remote-tracking ref (e.g., refs/remotes/origin/notes/my_namespace).
	// We fetch the specific notes ref. If it doesn't exist on the remote, fetch will indicate this.
	_, fetchStderr, fetchErr := executeGitCommand("fetch", remoteName, localRef)

	remoteNotesExist := true
	if fetchErr != nil {
		// Check if the error is because the remote ref simply doesn't exist.
		// This is common if notes haven't been pushed to this namespace on the remote yet.
		// `git fetch` often exits with status 1 or 128 for "ref not found".
		stderrLower := strings.ToLower(fetchStderr)
		if strings.Contains(stderrLower, "couldn't find remote ref") ||
			strings.Contains(stderrLower, "no such ref") ||
			strings.Contains(stderrLower, "fetch-pack: invalid refspec") ||
			(strings.Contains(fetchErr.Error(), "exit status 1") && fetchStderr == "") || // Can happen if ref not found
			strings.Contains(fetchErr.Error(), "exit status 128") {
			remoteNotesExist = false
		} else {
			// A more significant fetch error occurred.
			return fmt.Errorf("failed to fetch notes from remote '%s' for ref '%s' before merge: %w; stderr: %s",
				remoteName, localRef, fetchErr, fetchStderr)
		}
	}

	if remoteNotesExist {
		// 2. Determine the remote-tracking ref name to merge from.
		// Example: localRef="refs/notes/commits", remoteName="origin" -> remoteTrackingRef = "refs/remotes/origin/notes/commits"
		var remoteTrackingRef string
		if strings.HasPrefix(localRef, "refs/notes/") {
			pathSuffix := strings.TrimPrefix(localRef, "refs/") // e.g., "notes/commits" or "notes/my_ns_suffix"
			remoteTrackingRef = fmt.Sprintf("refs/remotes/%s/%s", remoteName, pathSuffix)
		} else {
			return fmt.Errorf("internal error: localRef '%s' is not in the expected 'refs/notes/...' format", localRef)
		}

		// Save the current local ref before merge attempt (for potential rollback)
		localRefSHA, _, err := executeGitCommand("rev-parse", localRef)
		if err != nil {
			// If local ref doesn't exist yet, that's okay
			localRefSHA = ""
		}

		// Verify the remote-tracking ref exists (it should if fetch was successful and remote had notes)
		_, _, errVerifyRemoteRef := executeGitCommand("rev-parse", "--verify", remoteTrackingRef)
		if errVerifyRemoteRef == nil {
			// 3. Merge fetched remote notes into local notes using 'cat_sort_uniq' strategy
			_, mergeStderr, mergeErr := executeGitCommand("notes", "--ref", localRef, "merge", "-s", "cat_sort_uniq", remoteTrackingRef)
			if mergeErr != nil {
				mergeStderrLower := strings.ToLower(mergeStderr)
				// "Already up to date" or "nothing to merge" are not errors in this context.
				if !strings.Contains(mergeStderrLower, "already up to date") &&
					!strings.Contains(mergeStderrLower, "nothing to merge") {

					// Abort the failed merge to clean up state
					_, _, _ = executeGitCommand("notes", "--ref", localRef, "merge", "--abort")

					// If we had a local ref before, reset to it
					if localRefSHA != "" {
						_, _, _ = executeGitCommand("update-ref", localRef, strings.TrimSpace(localRefSHA))
					}

					if strings.Contains(mergeStderrLower, "conflict") {
						return fmt.Errorf("failed to automatically merge notes from '%s' into '%s' using 'cat_sort_uniq', conflict: %w; stderr: %s",
							remoteTrackingRef, localRef, mergeErr, mergeStderr)
					}
					return fmt.Errorf("failed to merge notes from '%s' into '%s': %w; stderr: %s",
						remoteTrackingRef, localRef, mergeErr, mergeStderr)
				}
			}
		}
	}

	// 4. Push the (now potentially merged) local notes to the remote.
	// This push should ideally be a fast-forward.
	_, pushStderr, pushErr := executeGitCommand("push", remoteName, localRef)
	if pushErr != nil {
		// If this push still fails (e.g., non-fast-forward because someone *else* pushed notes
		// *between* our fetch and this push), then the situation is a race condition.
		// The user might need to re-run the operation.
		return fmt.Errorf("failed to push merged notes ref '%s' to remote '%s': %w; stderr: %s",
			localRef, remoteName, pushErr, pushStderr)
	}

	return nil
}

// SetNoteJSON serializes the given value to JSON and stores it as a git note
// using the generic type T for the value.
// This function overwrites any existing note for the given commitSha with this single JSON object.
func SetNoteJSON[T any](namespace, commitSha string, value T) error {
	if commitSha == "" {
		return fmt.Errorf("commitSha cannot be empty for SetNoteJSON")
	}
	// Serialize the value to JSON
	jsonData, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value of type %T to JSON for commit %s: %w", value, commitSha, err)
	}

	// Call the original SetNote with the JSON string
	return SetNote(namespace, commitSha, string(jsonData))
}

// GetNoteJSON retrieves a git note, which may contain one or more concatenated JSON objects.
// It deserializes each JSON object from the note content into elements of type T.
// T is the type of the elements in the returned slice (e.g., MyStruct or *MyStruct).
func GetNoteJSON[T any](namespace, commitSha string) ([]T, error) {
	if commitSha == "" {
		return nil, fmt.Errorf("commitSha cannot be empty for GetNoteJSON")
	}

	noteContent, err := GetNote(namespace, commitSha)
	if err != nil {
		// Check if the error is specifically because the note wasn't found.
		// `git notes show SHA` exits with 1 if no note for SHA.
		// Our GetNote wraps this. We look for "failed to get note" and underlying "exit status 1".
		if IsNoteNotFound(err) {
			// Consider if "exit status 1" from underlying git command is a reliable indicator.
			// If GetNote's error indicates the note simply doesn't exist, return an empty slice and no error.
			return nil, nil // Or []T{}, nil
		}
		return nil, fmt.Errorf("failed to get underlying note for commit %s: %w", commitSha, err)
	}

	if strings.TrimSpace(noteContent) == "" {
		return nil, nil // Or []T{}, nil - empty note content means no JSON objects
	}

	decoder := json.NewDecoder(strings.NewReader(noteContent))
	var results []T // Initialize as nil slice
	objectCount := 0

	for decoder.More() {
		if objectCount >= MaxJSONObjects {
			return results, fmt.Errorf("exceeded maximum number of JSON objects (%d) in note", MaxJSONObjects)
		}

		var currentElem T // Create a zero value of type T (e.g., MyStruct{} or nil for *MyStruct)

		// For pointer types T (e.g. *MyStruct), json.Decode needs a non-nil pointer to unmarshal into.
		// However, json.Unmarshal (and thus decoder.Decode) handles allocating the object if T is *MyStruct
		// and currentElem is initially nil. It will make currentElem point to the new MyStruct.
		// If T is MyStruct, &currentElem is *MyStruct, which is also what Decode expects.
		if errDecode := decoder.Decode(&currentElem); errDecode != nil {
			errMsgPrefix := fmt.Sprintf("failed to decode JSON object into %T in stream for commit %s (processed %d objects)", *new(T), commitSha, len(results))

			offset := decoder.InputOffset()
			snippetStart := int(offset) - 20
			if snippetStart < 0 {
				snippetStart = 0
			}
			snippetEnd := int(offset) + 20
			if snippetEnd > len(noteContent) {
				snippetEnd = len(noteContent)
			}

			var contextSnippet string
			if snippetStart < snippetEnd && snippetStart < len(noteContent) && snippetEnd <= len(noteContent) {
				contextSnippet = noteContent[snippetStart:snippetEnd]
			} else if snippetStart < len(noteContent) {
				contextSnippet = noteContent[snippetStart:]
			} else {
				contextSnippet = "(context unavailable)"
			}

			fullErrMsg := fmt.Sprintf("%s. Context around error (offset approx %d): \"...%s...\"", errMsgPrefix, offset, contextSnippet)
			// Return successfully decoded items so far, along with the error.
			return results, fmt.Errorf("%s: %w", fullErrMsg, errDecode)
		}
		results = append(results, currentElem)
		objectCount++
	}

	// If decoder.More() is false, the stream of valid JSON objects has ended.
	// Any residual non-JSON data would have caused an error in decoder.Decode() inside the loop.
	return results, nil
}
