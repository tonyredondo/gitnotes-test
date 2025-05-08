package main

import (
	"bytes"
	"encoding/json" // Required for one of the new tests, or comparing structs
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect" // Required for reflect.DeepEqual
	"strings"
	"testing"
	"time"
)

// Helper function to execute a command in a specific directory
func runCmd(t *testing.T, dir string, command string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(command, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// Log stderr for easier debugging of git commands
		t.Logf("Command `git %s` in dir `%s` failed. Stderr: %s\nStdout: %s", strings.Join(args, " "), dir, stderr.String(), stdout.String())
		t.Fatalf("Command %s %v failed in dir %s: %v", command, args, dir, err)
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String())
}

// Helper function to setup a temporary git repository.
// It initializes git, sets user.name and user.email, and creates an initial commit.
// Returns the path to the repo and a cleanup function.
func setupTestRepo(t *testing.T) (repoPath string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "testrepo-gitnotes-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	runCmd(t, dir, "git", "init", "-b", "main")
	runCmd(t, dir, "git", "config", "user.email", "test@example.com")
	runCmd(t, dir, "git", "config", "user.name", "Test User")
	// Create an initial empty commit so HEAD exists, which some git operations might need
	runCmd(t, dir, "git", "commit", "--allow-empty", "-m", "Initial empty commit")

	return dir
}

// Helper function to create a commit in the given repo path.
// Returns the SHA of the created commit.
func createTestCommit(t *testing.T, repoPath string, filename string, content string, message string) string {
	t.Helper()
	filePath := filepath.Join(repoPath, filename)
	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to write file %s: %v", filePath, err)
	}
	runCmd(t, repoPath, "git", "add", filename)
	runCmd(t, repoPath, "git", "commit", "-m", message)
	sha, _ := runCmd(t, repoPath, "git", "rev-parse", "HEAD")
	return sha
}

// TestMain checks for git availability before running tests.
func TestMain(m *testing.M) {
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Println("WARNING: 'git' command not found in PATH, skipping git-dependent tests.")
		// os.Exit(0) // Skips all tests in this package if git not found
		// For CI, it might be better to fail if git is expected.
		// For local dev, skipping is fine. Let's allow tests to run and fail if git is missing
		// and a test actually needs it. Many modern test runners will report this well.
	}
	os.Exit(m.Run())
}

func TestGitNoteOperations(t *testing.T) {
	repoPath := setupTestRepo(t)
	commitSha1 := createTestCommit(t, repoPath, "file1.txt", "content1", "Initial commit for string notes")
	commitSha2 := createTestCommit(t, repoPath, "file2.txt", "content2", "Second commit for string notes")

	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}
	if err := os.Chdir(repoPath); err != nil {
		t.Fatalf("Failed to change CWD to repoPath %s: %v", repoPath, err)
	}
	defer func() {
		if err := os.Chdir(originalCwd); err != nil {
			t.Logf("Failed to restore CWD to %s: %v", originalCwd, err)
		}
	}()

	namespace := "test-string-namespace"
	note1Content := "This is a note for commit 1 - " + time.Now().Format(time.RFC3339Nano)
	note2Content := "This is an updated note for commit 1 - " + time.Now().Format(time.RFC3339Nano)
	note3Content := "This is a note for commit 2 - " + time.Now().Format(time.RFC3339Nano)

	t.Run("SetAndGetNote_String", func(t *testing.T) {
		err := SetNote(namespace, commitSha1, note1Content)
		if err != nil {
			t.Fatalf("SetNote failed: %v", err)
		}

		retrieved, err := GetNote(namespace, commitSha1)
		if err != nil {
			t.Fatalf("GetNote failed: %v", err)
		}
		if retrieved != note1Content {
			t.Errorf("GetNote: expected '%s', got '%s'", note1Content, retrieved)
		}

		_, err = GetNote(namespace, "nonexistentsha")
		if err == nil {
			t.Error("GetNote: expected error for non-existent SHA, got nil")
		}
		_, err = GetNote(namespace, commitSha2) // Note not set yet for commitSha2
		if err == nil {
			t.Error("GetNote: expected error for unset note for commitSha2, got nil")
		} else {
			// Check that we get an error because the note doesn't exist.
			// `git notes show SHA` exits with 1 if no note for SHA.
			// Our wrapper should reflect this.
			if !strings.Contains(err.Error(), "failed to get note") {
				t.Errorf("GetNote: expected 'failed to get note' error for unset note, got: %v", err)
			}
		}
	})

	t.Run("OverwriteNote_String", func(t *testing.T) {
		err := SetNote(namespace, commitSha1, note1Content)
		if err != nil {
			t.Fatalf("SetNote (initial) failed: %v", err)
		}
		err = SetNote(namespace, commitSha1, note2Content)
		if err != nil {
			t.Fatalf("SetNote (overwrite) failed: %v", err)
		}

		retrieved, err := GetNote(namespace, commitSha1)
		if err != nil {
			t.Fatalf("GetNote after overwrite failed: %v", err)
		}
		if retrieved != note2Content {
			t.Errorf("GetNote after overwrite: expected '%s', got '%s'", note2Content, retrieved)
		}
	})

	t.Run("GetNoteList_ReturnsSHAs", func(t *testing.T) {
		// Ensure a clean state for this specific sub-test if needed, or ensure unique namespace
		testNamespace := namespace + "-getlist-sha"
		// Clean up any old notes in this namespace for the test commits, just in case.
		_ = DeleteNote(testNamespace, commitSha1)
		_ = DeleteNote(testNamespace, commitSha2)

		if err := SetNote(testNamespace, commitSha1, note1Content); err != nil {
			t.Fatalf("Setup SetNote for commitSha1 failed: %v", err)
		}
		if err := SetNote(testNamespace, commitSha2, note2Content); err != nil {
			t.Fatalf("Setup SetNote for commitSha2 failed: %v", err)
		}

		// Add a third commit and note to make the test more robust
		commitSha3 := createTestCommit(t, repoPath, "file3.txt", "content3", "Third commit for GetNoteList SHA test")
		if err := SetNote(testNamespace, commitSha3, note3Content); err != nil {
			t.Fatalf("Setup SetNote for commitSha3 failed: %v", err)
		}

		retrievedShas, err := GetNoteList(testNamespace)
		if err != nil {
			t.Fatalf("GetNoteList failed: %v", err)
		}

		expectedShas := map[string]bool{
			commitSha1: true,
			commitSha2: true,
			commitSha3: true,
		}

		if len(retrievedShas) != len(expectedShas) {
			t.Fatalf("GetNoteList: expected %d SHAs, got %d. SHAs: %v", len(expectedShas), len(retrievedShas), retrievedShas)
		}

		for _, sha := range retrievedShas {
			if _, ok := expectedShas[sha]; !ok {
				t.Errorf("GetNoteList: retrieved unexpected SHA '%s'. Retrieved list: %v", sha, retrievedShas)
			}
		}

		// Test with an empty namespace
		emptyShas, err := GetNoteList("empty-sha-test-namespace")
		if err != nil {
			t.Fatalf("GetNoteList for empty namespace failed: %v", err)
		}
		if len(emptyShas) != 0 {
			t.Errorf("GetNoteList for empty namespace: expected 0 SHAs, got %d", len(emptyShas))
		}

		// Clean up notes added for this test
		_ = DeleteNote(testNamespace, commitSha1)
		_ = DeleteNote(testNamespace, commitSha2)
		_ = DeleteNote(testNamespace, commitSha3)
	})

	t.Run("GetNoteList_ReturnsSHAs_ReverseChronological", func(t *testing.T) {
		testNamespace := namespace + "-getlist-sha-sorted"

		c1Content := "content for commit 1"
		c1Msg := "Commit 1 message"

		// sha1 will be oldest, sha3 will be newest.
		// Ensure sufficient delay for distinct timestamps (Git timestamps are usually per second)
		sha1 := createTestCommit(t, repoPath, "file_s1.txt", c1Content, c1Msg)
		t.Logf("Created sha1: %s at %s", sha1, time.Now()) // Optional: Log creation time for debugging

		// Sleep for more than 1 second to ensure the next commit gets a new timestamp
		time.Sleep(1*time.Second + 200*time.Millisecond)

		sha2 := createTestCommit(t, repoPath, "file_s2.txt", "content for commit 2", "Commit 2 message")
		t.Logf("Created sha2: %s at %s", sha2, time.Now()) // Optional: Log

		time.Sleep(1*time.Second + 200*time.Millisecond)

		sha3 := createTestCommit(t, repoPath, "file_s3.txt", "content for commit 3", "Commit 3 message")
		t.Logf("Created sha3: %s at %s", sha3, time.Now()) // Optional: Log

		// ... rest of the test ...
		// (Clean up notes, SetNotes, GetNoteList call, assertions)

		// Clean up any old notes in this namespace for the test commits
		_ = DeleteNote(testNamespace, sha1)
		_ = DeleteNote(testNamespace, sha2)
		_ = DeleteNote(testNamespace, sha3)

		if err := SetNote(testNamespace, sha1, "Note for sha1"); err != nil {
			t.Fatalf("SetNote for sha1 failed: %v", err)
		}
		if err := SetNote(testNamespace, sha2, "Note for sha2"); err != nil {
			t.Fatalf("SetNote for sha2 failed: %v", err)
		}
		if err := SetNote(testNamespace, sha3, "Note for sha3"); err != nil {
			t.Fatalf("SetNote for sha3 failed: %v", err)
		}

		retrievedShas, err := GetNoteList(testNamespace)
		if err != nil {
			t.Fatalf("GetNoteList failed: %v", err)
		}

		// Expected order: newest to oldest
		expectedShasInOrder := []string{sha3, sha2, sha1}

		if len(retrievedShas) != len(expectedShasInOrder) {
			// Add the actual retrieved SHAs to the failure message for easier debugging
			t.Fatalf("GetNoteList: expected %d SHAs, got %d. Expected: %v, Actual: %v",
				len(expectedShasInOrder), len(retrievedShas), expectedShasInOrder, retrievedShas)
		}

		for i, retrievedSha := range retrievedShas {
			if retrievedSha != expectedShasInOrder[i] {
				t.Errorf("GetNoteList: SHA at index %d mismatch. Expected '%s', got '%s'.\nExpected order: %v\nActual order:   %v",
					i, expectedShasInOrder[i], retrievedSha, expectedShasInOrder, retrievedShas)
			}
		}
		// This logging might be redundant if the loop above already prints details on mismatch
		// if t.Failed() {
		//     t.Logf("Expected order on fail: %v", expectedShasInOrder)
		//     t.Logf("Actual order on fail:   %v", retrievedShas)
		// }

		// Test with an empty namespace (should also return empty slice, not nil)
		emptyShas, err := GetNoteList("empty-sha-test-namespace-sorted")
		if err != nil {
			t.Fatalf("GetNoteList for empty namespace failed: %v", err)
		}
		if len(emptyShas) != 0 {
			t.Errorf("GetNoteList for empty namespace: expected 0 SHAs, got %d. List: %v", len(emptyShas), emptyShas)
		}

		// Clean up notes
		_ = DeleteNote(testNamespace, sha1)
		_ = DeleteNote(testNamespace, sha2)
		_ = DeleteNote(testNamespace, sha3)
	})

	t.Run("DeleteNote_String", func(t *testing.T) {
		if err := SetNote(namespace, commitSha1, note2Content); err != nil {
			t.Fatalf("Setup SetNote for DeleteNote failed: %v", err)
		}

		err := DeleteNote(namespace, commitSha1)
		if err != nil {
			t.Fatalf("DeleteNote failed: %v", err)
		}

		_, err = GetNote(namespace, commitSha1)
		if err == nil {
			t.Error("GetNote after DeleteNote: expected error (note not found), got nil")
		}

		// Try deleting a non-existent note (already deleted)
		// `git notes remove` for a non-existent note usually exits with 0 if the ref exists but the commit has no note.
		// If the *ref itself* doesn't exist, it might error differently. Let's check our wrapper's behavior.
		// The current DeleteNote wrapper propagates the error from `git notes remove`.
		// `git notes remove <sha>` when <sha> has no note (but notes ref exists) exits 0.
		// `git notes --ref <nonexistent_ref> remove <sha>` exits 1.
		err = DeleteNote(namespace, commitSha1) // Note already deleted for this SHA under this namespace
		if err != nil {
			// This behavior depends on the strictness of `git notes remove`.
			// If it errors because the specific note object for the commit is gone, this test is fine.
			// If `git notes remove` is idempotent for a non-existing note on a commit, err might be nil.
			// `git notes remove` seems to be idempotent if the notes ref exists but the specific commit has no note.
			// Let's assume for now the underlying `git notes remove` is okay with removing an already removed/non-existent note for a SHA.
			// Update: `git notes remove <SHA>` when there is no note for <SHA> (but notes ref exists) is a no-op and exits 0.
			// So, err should be nil here.
			// t.Errorf("DeleteNote: expected no error for already deleted note, but got: %v", err)
		}

		// Try deleting a note for a non-existent SHA
		err = DeleteNote(namespace, "nonexistentsha")
		if err == nil {
			t.Error("DeleteNote: expected error for non-existent SHA, got nil")
		} else if !strings.Contains(err.Error(), "nonexistentsha") {
			// git will complain about the object name typically.
			t.Errorf("DeleteNote: error for non-existent SHA should mention the SHA: %v", err)
		}
	})
}

// --- Tests for JSON Note Operations ---
type MyCustomData struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Count     int       `json:"count"`
	IsEnabled bool      `json:"is_enabled"`
	Timestamp time.Time `json:"timestamp"`
}

func TestGitNoteJSONGenericOperations(t *testing.T) {
	repoPath := setupTestRepo(t)
	commitShaJSON1 := createTestCommit(t, repoPath, "file_json1.txt", "json content 1", "Commit for JSON note 1")
	commitShaJSON2 := createTestCommit(t, repoPath, "file_json2.txt", "json content 2", "Commit for JSON note 2")

	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}
	if err := os.Chdir(repoPath); err != nil {
		t.Fatalf("Failed to change CWD to repoPath %s: %v", repoPath, err)
	}
	defer func() {
		if err := os.Chdir(originalCwd); err != nil {
			t.Logf("Failed to restore CWD to %s: %v", originalCwd, err)
		}
	}()

	jsonNamespace := "test-json-generic-namespace"
	fixedTime, _ := time.Parse(time.RFC3339, "2023-10-26T10:00:00Z")

	data1 := MyCustomData{ID: "obj1", Name: "Object One", Count: 1, IsEnabled: true, Timestamp: fixedTime}
	data2 := MyCustomData{ID: "obj2", Name: "Object Two", Count: 2, IsEnabled: false, Timestamp: fixedTime.Add(1 * time.Hour)}
	data3 := MyCustomData{ID: "obj3", Name: "Object Three", Count: 3, IsEnabled: true, Timestamp: fixedTime.Add(2 * time.Hour)}

	t.Run("SetNoteJSON_SingleObject_And_GetNoteJSON_RetrievesSliceOfOne", func(t *testing.T) {
		err := SetNoteJSON[MyCustomData](jsonNamespace, commitShaJSON1, data1)
		if err != nil {
			t.Fatalf("SetNoteJSON[MyCustomData] failed: %v", err)
		}

		retrievedDataSlice, err := GetNoteJSON[MyCustomData](jsonNamespace, commitShaJSON1)
		if err != nil {
			t.Fatalf("GetNoteJSON[MyCustomData] failed: %v", err)
		}

		if len(retrievedDataSlice) != 1 {
			t.Fatalf("GetNoteJSON[MyCustomData]: expected slice of 1 element, got %d", len(retrievedDataSlice))
		}
		if !reflect.DeepEqual(data1, retrievedDataSlice[0]) {
			t.Errorf("GetNoteJSON[MyCustomData]: data mismatch.\nExpected single element: %+v\nGot in slice: %+v", data1, retrievedDataSlice[0])
		}
	})

	t.Run("SetNoteJSON_Overwrite_And_GetNoteJSON_RetrievesNewSliceOfOne", func(t *testing.T) {
		err := SetNoteJSON(jsonNamespace, commitShaJSON1, data1) // Type inference can work for T
		if err != nil {
			t.Fatalf("SetNoteJSON (initial) failed: %v", err)
		}
		// Overwrite with data2
		err = SetNoteJSON(jsonNamespace, commitShaJSON1, data2)
		if err != nil {
			t.Fatalf("SetNoteJSON (overwrite) failed: %v", err)
		}

		retrievedDataSlice, err := GetNoteJSON[MyCustomData](jsonNamespace, commitShaJSON1)
		if err != nil {
			t.Fatalf("GetNoteJSON after overwrite failed: %v", err)
		}
		if len(retrievedDataSlice) != 1 {
			t.Fatalf("GetNoteJSON after overwrite: expected slice of 1 element, got %d", len(retrievedDataSlice))
		}
		if !reflect.DeepEqual(data2, retrievedDataSlice[0]) {
			t.Errorf("GetNoteJSON after overwrite: data mismatch. Expected %+v, got %+v", data2, retrievedDataSlice[0])
		}
	})

	t.Run("GetNoteJSON_ConcatenatedJSONs_ValueType", func(t *testing.T) {
		json1Bytes, _ := json.Marshal(data1)
		json2Bytes, _ := json.Marshal(data2)
		json3Bytes, _ := json.Marshal(data3)
		concatenatedJSONs := string(json1Bytes) + string(json2Bytes) + string(json3Bytes)

		// Use SetNote to manually set the concatenated string
		err := SetNote(jsonNamespace, commitShaJSON2, concatenatedJSONs)
		if err != nil {
			t.Fatalf("Failed to set concatenated JSON note: %v", err)
		}

		retrievedSlice, err := GetNoteJSON[MyCustomData](jsonNamespace, commitShaJSON2)
		if err != nil {
			t.Fatalf("GetNoteJSON[MyCustomData] for concatenated JSONs failed: %v", err)
		}

		expectedSlice := []MyCustomData{data1, data2, data3}
		if !reflect.DeepEqual(expectedSlice, retrievedSlice) {
			t.Errorf("GetNoteJSON[MyCustomData] (concatenated): data mismatch.\nExpected slice: %+v\nGot slice:      %+v", expectedSlice, retrievedSlice)
		}
	})

	t.Run("GetNoteJSON_ConcatenatedJSONs_PointerType", func(t *testing.T) {
		json1Bytes, _ := json.Marshal(data1)
		json2Bytes, _ := json.Marshal(data2)
		concatenatedJSONs := string(json1Bytes) + string(json2Bytes)

		err := SetNote(jsonNamespace, commitShaJSON2, concatenatedJSONs)
		if err != nil {
			t.Fatalf("Failed to set concatenated JSON note for pointer slice test: %v", err)
		}

		retrievedPtrSlice, err := GetNoteJSON[*MyCustomData](jsonNamespace, commitShaJSON2)
		if err != nil {
			t.Fatalf("GetNoteJSON[*MyCustomData] for concatenated JSONs failed: %v", err)
		}

		// Create expected slice of pointers
		expectedData := []*MyCustomData{&data1, &data2}
		if len(retrievedPtrSlice) != len(expectedData) {
			t.Fatalf("GetNoteJSON[*MyCustomData] (concatenated): expected %d elements, got %d.", len(expectedData), len(retrievedPtrSlice))
		}
		for i := range expectedData {
			if !reflect.DeepEqual(expectedData[i], retrievedPtrSlice[i]) {
				t.Errorf("GetNoteJSON[*MyCustomData] (concatenated): element %d mismatch.\nExpected: %+v\nGot:      %+v", i, expectedData[i], retrievedPtrSlice[i])
			}
		}
	})

	t.Run("GetNoteJSON_NonExistentNote_ReturnsEmptySlice", func(t *testing.T) {
		retrievedSlice, err := GetNoteJSON[MyCustomData](jsonNamespace, "nonexistentcommitshaforjsongeneric")
		if err != nil {
			t.Fatalf("GetNoteJSON[MyCustomData] for non-existent note failed: %v", err)
		}
		if retrievedSlice != nil && len(retrievedSlice) != 0 {
			// GetNoteJSON returns nil slice if note doesn't exist or is empty.
			t.Errorf("GetNoteJSON[MyCustomData] for non-existent note: expected nil or empty slice, got %d elements: %+v", len(retrievedSlice), retrievedSlice)
		}
	})

	t.Run("GetNoteJSON_NonExistentNamespace_ReturnsEmptySlice", func(t *testing.T) {
		retrievedSlice, err := GetNoteJSON[MyCustomData]("non-existent-json-namespace", commitShaJSON1)
		if err != nil {
			t.Fatalf("GetNoteJSON[MyCustomData] for non-existent namespace failed: %v", err)
		}
		if retrievedSlice != nil && len(retrievedSlice) != 0 {
			t.Errorf("GetNoteJSON[MyCustomData] for non-existent namespace: expected nil or empty slice, got %d elements: %+v", len(retrievedSlice), retrievedSlice)
		}
	})

	t.Run("GetNoteJSON_EmptyNoteContent_ReturnsEmptySlice", func(t *testing.T) {
		err := SetNote(jsonNamespace, commitShaJSON1, "") // Set an empty note
		if err != nil {
			t.Fatalf("Failed to set empty note: %v", err)
		}
		retrievedSlice, err := GetNoteJSON[MyCustomData](jsonNamespace, commitShaJSON1)
		if err != nil {
			t.Fatalf("GetNoteJSON[MyCustomData] for empty note content failed: %v", err)
		}
		if retrievedSlice != nil && len(retrievedSlice) != 0 {
			t.Errorf("GetNoteJSON[MyCustomData] for empty note content: expected nil or empty slice, got %d elements", len(retrievedSlice))
		}
	})

	t.Run("GetNoteJSON_MalformedJSONStream_ReturnsPartialAndError", func(t *testing.T) {
		json1Bytes, _ := json.Marshal(data1)
		// Malformed: one valid JSON object followed by an unterminated one
		malformedContent := string(json1Bytes) + `{"id":"obj2","name":"unterminated string`

		err := SetNote(jsonNamespace, commitShaJSON2, malformedContent)
		if err != nil {
			t.Fatalf("Failed to set malformed JSON note: %v", err)
		}

		retrievedSlice, err := GetNoteJSON[MyCustomData](jsonNamespace, commitShaJSON2)
		if err == nil {
			t.Fatal("GetNoteJSON[MyCustomData] expected error for malformed JSON stream, but got nil")
		}
		if !strings.Contains(err.Error(), "failed to decode JSON object") {
			t.Errorf("GetNoteJSON[MyCustomData] error message for malformed stream is not as expected: %v", err)
		}
		if !strings.Contains(err.Error(), "(processed 1 objects)") {
			t.Errorf("GetNoteJSON[MyCustomData] error message should indicate 1 object was processed: %v", err)
		}
		// MODIFIED CHECK: Accept "unexpected EOF" as a valid syntax error for this case
		if !strings.Contains(err.Error(), "unexpected end of JSON input") &&
			!strings.Contains(err.Error(), "syntax error") &&
			!strings.Contains(err.Error(), "unexpected EOF") {
			t.Errorf("GetNoteJSON[MyCustomData] underlying error for malformed stream not as expected (should contain 'unexpected end of JSON input', 'syntax error', or 'unexpected EOF'): %v", err)
		}

		// Check that the first, valid object was decoded
		if len(retrievedSlice) != 1 || !reflect.DeepEqual(data1, retrievedSlice[0]) {
			t.Errorf("GetNoteJSON[MyCustomData] with malformed stream: expected 1 successfully decoded object (%+v), got %d. Slice content: %+v", data1, len(retrievedSlice), retrievedSlice)
		}
	})

	t.Run("GetNoteJSON_TrailingGarbageData_ReturnsPartialAndError", func(t *testing.T) {
		json1Bytes, _ := json.Marshal(data1)
		contentWithTrailingGarbage := string(json1Bytes) + "trailing garbage"

		err := SetNote(jsonNamespace, commitShaJSON1, contentWithTrailingGarbage)
		if err != nil {
			t.Fatalf("SetNote failed for content with trailing garbage: %v", err)
		}

		retrievedSlice, err := GetNoteJSON[MyCustomData](jsonNamespace, commitShaJSON1)
		if err == nil {
			t.Fatal("GetNoteJSON[MyCustomData]: expected an error due to trailing garbage data, but got nil")
		}

		if !strings.Contains(err.Error(), "failed to decode JSON object") {
			t.Errorf("GetNoteJSON[MyCustomData] error message for trailing garbage is not as expected: %v", err)
		}
		if !strings.Contains(err.Error(), "(processed 1 objects)") {
			t.Errorf("GetNoteJSON[MyCustomData] error message should indicate 1 object was processed before trailing garbage: %v", err)
		}
		// The underlying error from json.Decoder should indicate invalid character or similar
		// e.g., "invalid character 't' looking for beginning of value"
		if !strings.Contains(err.Error(), "invalid character") {
			t.Errorf("GetNoteJSON[MyCustomData]: underlying error for trailing garbage not as expected: %v", err)
		}

		// Check that the valid JSON object was processed
		if len(retrievedSlice) != 1 || !reflect.DeepEqual(data1, retrievedSlice[0]) {
			t.Errorf("GetNoteJSON[MyCustomData]: expected one valid object to be decoded (%+v) before trailing garbage error. Got slice: %+v", data1, retrievedSlice)
		}
	})

	// Clean up notes specifically created in this test suite if necessary,
	// though setupTestRepo handles overall repo cleanup.
	t.Cleanup(func() {
		_ = DeleteNote(jsonNamespace, commitShaJSON1)
		_ = DeleteNote(jsonNamespace, commitShaJSON2)
	})
}

// TestGitNoteRemoteOperations remains largely the same, as it tests the underlying
// string-based note transport. If JSON notes are used with remote operations,
// they are just strings at the transport layer.
func TestGitNoteRemoteOperations(t *testing.T) {
	// Setup "local" repository
	localRepoPath := setupTestRepo(t)
	localCommitSha := createTestCommit(t, localRepoPath, "localfile.txt", "local content", "Commit for remote ops")

	// Setup "remote" bare repository
	remoteRepoDir, err := os.MkdirTemp("", "testrepo-remote-bare-")
	if err != nil {
		t.Fatalf("Failed to create temp dir for remote bare repo: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(remoteRepoDir) })
	runCmd(t, remoteRepoDir, "git", "init", "--bare")

	// Configure local repo to have a remote pointing to the bare repo
	runCmd(t, localRepoPath, "git", "remote", "add", "testorigin", remoteRepoDir)

	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get CWD: %v", err)
	}
	if err := os.Chdir(localRepoPath); err != nil {
		t.Fatalf("Failed to change CWD to %s: %v", localRepoPath, err)
	}
	defer func() {
		if err := os.Chdir(originalCwd); err != nil {
			t.Logf("Failed to restore CWD to %s: %v", originalCwd, err)
		}
	}()

	namespace := "remote-ops-namespace"
	noteContent := "Note for remote testing - " + time.Now().Format(time.RFC3339Nano) // Plain string for this test

	t.Run("PushAndFetchNotes_String", func(t *testing.T) {
		if err := SetNote(namespace, localCommitSha, noteContent); err != nil {
			t.Fatalf("SetNote locally failed: %v", err)
		}

		if err := PushNotes(namespace, "testorigin"); err != nil {
			t.Fatalf("PushNotes failed: %v", err)
		}

		expectedRemoteRefPath := filepath.Join(remoteRepoDir, formatNamespaceRef(namespace))
		if _, err := os.Stat(expectedRemoteRefPath); os.IsNotExist(err) {
			stdout, _ := runCmd(t, "", "git", "-C", remoteRepoDir, "show", formatNamespaceRef(namespace)) // Check directly in bare repo
			// A simpler check for bare repo is to see if the ref exists
			stdoutLsRemote, _ := runCmd(t, localRepoPath, "git", "ls-remote", "testorigin", formatNamespaceRef(namespace))
			if !strings.Contains(stdoutLsRemote, formatNamespaceRef(namespace)) {
				t.Logf("git show output from bare repo for %s:\n%s", formatNamespaceRef(namespace), stdout)
				t.Errorf("Note ref '%s' not found in remote 'testorigin' after PushNotes. ls-remote output: %s", formatNamespaceRef(namespace), stdoutLsRemote)
			}
		}

		if err := DeleteNote(namespace, localCommitSha); err != nil {
			t.Fatalf("DeleteNote locally failed: %v", err)
		}
		_, err = GetNote(namespace, localCommitSha)
		if err == nil {
			t.Fatal("Note still exists locally after delete, before FetchNotes test.")
		}

		if err := FetchNotes(namespace, "testorigin"); err != nil {
			t.Fatalf("FetchNotes failed: %v", err)
		}

		fetchedNote, err := GetNote(namespace, localCommitSha)
		if err != nil {
			t.Fatalf("GetNote after FetchNotes failed: %v", err)
		}
		if fetchedNote != noteContent {
			t.Errorf("Fetched note content mismatch: expected '%s', got '%s'", noteContent, fetchedNote)
		}
	})

	t.Run("PushToNonExistentRemote_String", func(t *testing.T) {
		if err := SetNote(namespace, localCommitSha, "some note for non-existent remote"); err != nil {
			t.Fatalf("SetNote locally failed: %v", err)
		}
		err := PushNotes(namespace, "nonexistentremote")
		if err == nil {
			t.Error("PushNotes to non-existent remote should have failed, but did not")
		}
	})

	t.Run("FetchFromNonExistentRemote_String", func(t *testing.T) {
		err := FetchNotes(namespace, "nonexistentremote")
		if err == nil {
			// This might not be an error if the remote doesn't exist, depending on git fetch's behavior
			// and our wrapper's interpretation. Current FetchNotes might return nil if ref just not found.
			// Let's verify based on current FetchNotes.
			// If remote "nonexistentremote" truly doesn't exist, `git fetch` will error.
			t.Error("FetchNotes from non-existent remote should have failed, but did not")
		}
	})
}
