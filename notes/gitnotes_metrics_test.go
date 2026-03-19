package notes

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestGitCommandMetricsHook_CapturesSubcommands(t *testing.T) {
	repoPath := setupTestRepo(t)
	sha := createTestCommit(t, repoPath, "metrics.txt", "content", "metrics commit")

	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	if err := os.Chdir(repoPath); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(originalCwd) }()

	collector := newCommandMetricsCollector()
	restore := setGitCommandMetricsHookForTesting(collector)
	defer restore()

	manager := NewNotesManager("metrics-namespace")
	if err := manager.SetNote(sha, "hello"); err != nil {
		t.Fatalf("SetNote failed: %v", err)
	}
	if _, err := manager.GetNote(sha); err != nil {
		t.Fatalf("GetNote failed: %v", err)
	}
	_, errs := manager.GetNotesBulk([]string{sha})
	if len(errs) > 0 {
		t.Fatalf("GetNotesBulk failed: %v", errs)
	}

	metrics := strings.Join(collector.Snapshot(), "\n")
	if !strings.Contains(metrics, "notes add") {
		t.Fatalf("expected notes add metrics, got: %s", metrics)
	}
	if !strings.Contains(metrics, "notes show") {
		t.Fatalf("expected notes show metrics, got: %s", metrics)
	}
	if !strings.Contains(metrics, "notes list") {
		t.Fatalf("expected notes list metrics, got: %s", metrics)
	}
}

func TestGetNotesBulk_UsesBatchPathWithoutFallback(t *testing.T) {
	repoPath := setupTestRepo(t)
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	if err := os.Chdir(repoPath); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(originalCwd) }()

	managerIface := NewNotesManager("batch-path-test")
	manager := managerIface.(*notesManager)
	shas := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		sha := createTestCommit(t, repoPath, fmt.Sprintf("batch-%d.txt", i), fmt.Sprintf("value-%d", i), fmt.Sprintf("msg-%d", i))
		if err := manager.SetNote(sha, fmt.Sprintf("note-%d", i)); err != nil {
			t.Fatalf("failed to seed note %d: %v", i, err)
		}
		shas = append(shas, sha)
	}

	collector := newCommandMetricsCollector()
	restore := setGitCommandMetricsHookForTesting(collector)
	defer restore()

	listOutput, _, err := executeGitCommand("notes", "--ref", manager.ref, "list")
	if err != nil {
		t.Fatalf("notes list failed: %v", err)
	}
	commitToObj := parseNoteListToCommitMap(listOutput)
	objects := make([]string, 0, len(shas))
	objToCommit := make(map[string]string, len(shas))
	for _, sha := range shas {
		obj := commitToObj[sha]
		objects = append(objects, obj)
		objToCommit[obj] = sha
	}
	catOutput, _, err := executeGitCommandWithStdin(strings.Join(objects, "\n")+"\n", "cat-file", "--batch")
	if err != nil {
		t.Fatalf("cat-file --batch failed: %v", err)
	}
	if _, _, err := parseCatFileBatchOutput(catOutput, objToCommit); err != nil {
		t.Fatalf("parseCatFileBatchOutput failed: %v", err)
	}

	results, errs := manager.GetNotesBulk(shas)
	if len(errs) > 0 {
		t.Fatalf("GetNotesBulk returned errors: %v", errs)
	}
	if len(results) != len(shas) {
		t.Fatalf("expected %d results, got %d", len(shas), len(results))
	}

	metrics := strings.Join(collector.Snapshot(), "\n")
	if !strings.Contains(metrics, "cat-file --batch") {
		t.Fatalf("expected batch command in metrics, got: %s", metrics)
	}
	if strings.Contains(metrics, "notes show") {
		t.Fatalf("expected no fallback notes show calls, got: %s", metrics)
	}
}
