package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	// "time" // Only needed for the main example, not the core API
)

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

// executeGitCommand is a helper function to run git commands and capture their output and errors.
// It returns stdout, stderr, and an error.
func executeGitCommand(args ...string) (string, string, error) {
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("git %s failed: %w; stderr: %s", args[0], err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), nil
}

// GetNote retrieves the content of a note for a specific commit SHA in a namespace.
func GetNote(namespace, commitSha string) (string, error) {
	if commitSha == "" {
		return "", fmt.Errorf("commitSha cannot be empty")
	}
	ref := formatNamespaceRef(namespace)
	stdout, _, err := executeGitCommand("notes", "--ref", ref, "show", commitSha)
	if err != nil {
		return "", fmt.Errorf("failed to get note for %s in %s: %w", commitSha, ref, err)
	}
	return stdout, nil
}

// SetNote sets (or overwrites) a note for a specific commit SHA in a namespace.
func SetNote(namespace, commitSha, value string) error {
	if commitSha == "" {
		return fmt.Errorf("commitSha cannot be empty")
	}
	ref := formatNamespaceRef(namespace)
	_, stderrOutput, err := executeGitCommand("notes", "--ref", ref, "add", "-f", "-m", value, commitSha)
	if err != nil {
		return fmt.Errorf("failed to set note for %s in %s (stderr: %s): %w", commitSha, ref, stderrOutput, err)
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
	var commitsWithNotes []commitInfo

	listOutput, _, err := executeGitCommand("notes", "--ref", ref, "list")
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "bad notes ref") || strings.Contains(errMsg, "does not exist") || strings.Contains(errMsg, "no notes found") {
			return []string{}, nil // Return empty slice, not nil, for consistency
		}
		return nil, fmt.Errorf("failed to list notes in %s: %w", ref, err)
	}

	if listOutput == "" {
		return []string{}, nil // No notes in this namespace
	}

	scanner := bufio.NewScanner(strings.NewReader(listOutput))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			commitSha := parts[1] // The commit SHA

			// Get the committer timestamp for this SHA
			// %ct gives committer date, UNIX timestamp
			tsOutput, _, tsErr := executeGitCommand("show", "-s", "--format=%ct", commitSha)
			if tsErr != nil {
				// If we can't get the timestamp, we might skip this commit or handle error
				// For now, let's return an error as it disrupts sorting.
				return nil, fmt.Errorf("failed to get timestamp for commit %s: %w", commitSha, tsErr)
			}
			timestamp, convErr := strconv.ParseInt(strings.TrimSpace(tsOutput), 10, 64)
			if convErr != nil {
				return nil, fmt.Errorf("failed to parse timestamp for commit %s ('%s'): %w", commitSha, tsOutput, convErr)
			}
			commitsWithNotes = append(commitsWithNotes, commitInfo{Sha: commitSha, Timestamp: timestamp})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning 'git notes list' output for %s: %w", ref, err)
	}

	// Sort the commits by timestamp in descending order (newest first)
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
	ref := formatNamespaceRef(namespace)
	_, stderrOutput, err := executeGitCommand("notes", "--ref", ref, "remove", commitSha)
	if err != nil {
		return fmt.Errorf("failed to delete note for %s in %s (stderr: %s): %w", commitSha, ref, stderrOutput, err)
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
	fullRefSpec := fmt.Sprintf("%s:%s", localRef, localRef) // local_ref:remote_ref, but same name

	_, stderrOutput, err := executeGitCommand("fetch", "--force", remoteName, fullRefSpec)
	if err != nil {
		return fmt.Errorf("failed to fetch notes for namespace %s (refspec %s) from %s (stderr: %s): %w", namespace, fullRefSpec, remoteName, stderrOutput, err)
	}
	return nil
}

// PushNotes fetches remote notes for the given namespace, merges them into the local notes
// using the 'cat_sort_uniq' strategy, and then pushes the combined result to the remote.
func PushNotes(namespace, remoteName string) error {
	localRef := formatNamespaceRef(namespace) // e.g., refs/notes/my_namespace

	// 1. Fetch remote notes. This updates the remote-tracking ref (e.g., refs/remotes/origin/notes/my_namespace).
	// We fetch the specific notes ref. If it doesn't exist on the remote, fetch will indicate this.
	// fmt.Printf("PushNotes: Fetching remote notes for '%s' from '%s'...\n", localRef, remoteName)
	_, fetchStderr, fetchErr := executeGitCommand("fetch", remoteName, localRef)

	remoteNotesExist := true
	if fetchErr != nil {
		// Check if the error is because the remote ref simply doesn't exist.
		// This is common if notes haven't been pushed to this namespace on the remote yet.
		// `git fetch` often exits with status 1 or 128 for "ref not found".
		if strings.Contains(strings.ToLower(fetchStderr), "couldn't find remote ref") ||
			strings.Contains(strings.ToLower(fetchStderr), "no such ref") ||
			strings.Contains(strings.ToLower(fetchStderr), "fetch-pack: invalid refspec") ||
			(strings.Contains(fetchErr.Error(), "exit status 1") && fetchStderr == "") || // Can happen if ref not found
			strings.Contains(fetchErr.Error(), "exit status 128") {
			// fmt.Printf("PushNotes: Info: Remote '%s' does not have notes ref '%s'. No remote notes to merge.\n", remoteName, localRef)
			remoteNotesExist = false
		} else {
			// A more significant fetch error occurred.
			return fmt.Errorf("failed to fetch notes from remote '%s' for ref '%s' before merge: %w; stderr: %s", remoteName, localRef, fetchErr, fetchStderr)
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
			return fmt.Errorf("internal programming error: localRef '%s' is not in the expected 'refs/notes/...' format", localRef)
		}

		// Verify the remote-tracking ref exists (it should if fetch was successful and remote had notes)
		_, _, errVerifyRemoteRef := executeGitCommand("rev-parse", "--verify", remoteTrackingRef)
		if errVerifyRemoteRef != nil {
			// This might happen if the remote ref truly doesn't exist and fetch indicated so,
			// or if the remote-tracking ref naming convention is different than expected.
			// fmt.Printf("PushNotes: Info: Remote tracking ref '%s' not found after fetch. Assuming no remote notes to merge.\n", remoteTrackingRef)
		} else {
			// 3. Merge fetched remote notes into local notes using 'cat_sort_uniq' strategy.
			// fmt.Printf("PushNotes: Merging notes from '%s' into local '%s' using 'cat_sort_uniq' strategy...\n", remoteTrackingRef, localRef)
			_, mergeStderr, mergeErr := executeGitCommand("notes", "--ref", localRef, "merge", "-s", "cat_sort_uniq", remoteTrackingRef)
			if mergeErr != nil {
				// "Already up to date" or "nothing to merge" are not errors in this context.
				if strings.Contains(strings.ToLower(mergeStderr), "already up to date") || strings.Contains(strings.ToLower(mergeStderr), "nothing to merge") {
					// fmt.Printf("PushNotes: Info: Local notes '%s' already incorporate or are ahead of '%s'.\n", localRef, remoteTrackingRef)
				} else if strings.Contains(strings.ToLower(mergeStderr), "conflict") {
					// Even with 'cat_sort_uniq', fundamental conflicts could theoretically occur, or the merge command failed.
					return fmt.Errorf("failed to automatically merge notes from '%s' into '%s' using 'cat_sort_uniq', possible conflict: %w; stderr: %s", remoteTrackingRef, localRef, mergeErr, mergeStderr)
				} else {
					return fmt.Errorf("failed to merge notes from '%s' into '%s': %w; stderr: %s", remoteTrackingRef, localRef, mergeErr, mergeStderr)
				}
			} else {
				// fmt.Printf("PushNotes: Successfully merged remote notes into '%s'.\n", localRef)
			}
		}
	}

	// 4. Push the (now potentially merged) local notes to the remote.
	// This push should ideally be a fast-forward.

	// fmt.Printf("PushNotes: Pushing local notes ref '%s' to remote '%s'...\n", localRef, remoteName)
	_, pushStderr, pushErr := executeGitCommand("push", remoteName, localRef)
	if pushErr != nil {
		// If this push still fails (e.g., non-fast-forward because someone *else* pushed notes
		// *between* our fetch and this push), then the situation is a race condition.
		// The user might need to re-run the operation.
		return fmt.Errorf("failed to push merged notes ref '%s' to remote '%s': %w; stderr: %s", localRef, remoteName, pushErr, pushStderr)
	}

	// fmt.Printf("PushNotes: Notes ref '%s' successfully pushed to remote '%s'.\n", localRef, remoteName)
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
		if strings.Contains(err.Error(), "failed to get note") {
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

	for decoder.More() {
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
	}

	// If decoder.More() is false, the stream of valid JSON objects has ended.
	// Any residual non-JSON data would have caused an error in decoder.Decode() inside the loop.
	return results, nil
}
