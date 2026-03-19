package notes

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Benchmarks for end-to-end NotesManager calls.
// These benchmarks include git process execution time because each API call shells out to git.

type commandMetricsCollector struct {
	mu          sync.Mutex
	totals      map[string]time.Duration
	counts      map[string]int
	totalCalls  int
	totalTiming time.Duration
}

func newCommandMetricsCollector() *commandMetricsCollector {
	return &commandMetricsCollector{
		totals: make(map[string]time.Duration),
		counts: make(map[string]int),
	}
}

func (c *commandMetricsCollector) OnCommandStart(_ []string) {}

func (c *commandMetricsCollector) OnCommandEnd(args []string, duration time.Duration, _ error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := gitCommandLabel(args)
	c.totals[key] += duration
	c.counts[key]++
	c.totalCalls++
	c.totalTiming += duration
}

func (c *commandMetricsCollector) Snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]string, 0, len(c.totals))
	for key := range c.totals {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys)+1)
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s calls=%d total=%s avg=%s", key, c.counts[key], c.totals[key], c.totals[key]/time.Duration(c.counts[key])))
	}
	lines = append(lines, fmt.Sprintf("total_calls=%d total_time=%s", c.totalCalls, c.totalTiming))
	return lines
}

func gitCommandLabel(args []string) string {
	if len(args) == 0 {
		return "git"
	}
	if args[0] == "notes" && len(args) >= 4 && args[1] == "--ref" {
		return "notes " + args[3]
	}
	if args[0] == "show" && len(args) >= 3 && args[1] == "-s" {
		return "show -s"
	}
	if len(args) >= 2 {
		return args[0] + " " + args[1]
	}
	return args[0]
}

func benchmarkManagerForCalls(b *testing.B, namespace string) (NotesManager, string, string) {
	b.Helper()
	repoPath := setupTestRepo(b)
	sha := createTestCommit(b, repoPath, "benchmark.txt", "benchmark", "benchmark commit")

	originalCwd, err := os.Getwd()
	if err != nil {
		b.Fatalf("failed to get current working directory: %v", err)
	}
	if err := os.Chdir(repoPath); err != nil {
		b.Fatalf("failed to change CWD to repoPath %s: %v", repoPath, err)
	}
	b.Cleanup(func() {
		_ = os.Chdir(originalCwd)
	})

	manager := NewNotesManager(namespace)
	if err := manager.SetNote(sha, "seed note"); err != nil {
		b.Fatalf("failed to seed note: %v", err)
	}

	sha2 := createTestCommit(b, repoPath, "benchmark2.txt", "benchmark-2", "benchmark commit 2")
	if err := manager.SetNote(sha2, "seed note 2"); err != nil {
		b.Fatalf("failed to seed second note: %v", err)
	}

	return manager, sha, sha2
}

func benchmarkManagerWithCountedNotes(b *testing.B, namespace string, noteCount int) (NotesManager, []string) {
	b.Helper()
	repoPath := setupTestRepo(b)

	originalCwd, err := os.Getwd()
	if err != nil {
		b.Fatalf("failed to get current working directory: %v", err)
	}
	if err := os.Chdir(repoPath); err != nil {
		b.Fatalf("failed to change CWD to repoPath %s: %v", repoPath, err)
	}
	b.Cleanup(func() {
		_ = os.Chdir(originalCwd)
	})

	manager := NewNotesManager(namespace)
	shas := make([]string, 0, noteCount)
	for i := 0; i < noteCount; i++ {
		sha := createTestCommit(b, repoPath, fmt.Sprintf("bench-list-%d.txt", i), fmt.Sprintf("value-%d", i), fmt.Sprintf("commit-%d", i))
		if err := manager.SetNote(sha, fmt.Sprintf("note-%d", i)); err != nil {
			b.Fatalf("failed to seed note %d: %v", i, err)
		}
		shas = append(shas, sha)
	}

	return manager, shas
}

func benchmarkWithCommandMetrics(b *testing.B, fn func(b *testing.B)) {
	collector := newCommandMetricsCollector()
	restore := setGitCommandMetricsHookForTesting(collector)
	b.Cleanup(restore)

	fn(b)

	b.StopTimer()
	for _, line := range collector.Snapshot() {
		b.Logf("git-metrics: %s", line)
	}
	b.StartTimer()
}

// legacyGetNotesBulk emulates the previous implementation (parallel per-SHA GetNote calls).
func legacyGetNotesBulk(manager *notesManager, commitShas []string) (map[string]string, map[string]error) {
	results := make(map[string]string)
	errs := make(map[string]error)

	for _, sha := range commitShas {
		if err := validateCommitSHA(sha); err != nil {
			errs[sha] = err
		}
	}

	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, sha := range commitShas {
		if _, hasError := errs[sha]; hasError {
			continue
		}
		wg.Add(1)
		go func(commitSha string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			note, err := manager.GetNote(commitSha)
			mu.Lock()
			if err != nil {
				errs[commitSha] = err
			} else {
				results[commitSha] = note
			}
			mu.Unlock()
		}(sha)
	}

	wg.Wait()
	return results, errs
}

// legacyGetNoteListUncached emulates the prior no-cache list implementation.
func legacyGetNoteListUncached(manager *notesManager) ([]string, error) {
	listOutput, _, err := executeGitCommand("notes", "--ref", manager.ref, "list")
	if err != nil {
		if gitErr := extractGitCommandError(err); gitErr != nil && errorMatcher.IsNotesRefNotFoundError(gitErr.ExitCode, gitErr.Stderr) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to list notes in %s: %w", manager.ref, err)
	}
	if listOutput == "" {
		return []string{}, nil
	}

	commitShas := make([]string, 0, strings.Count(listOutput, "\n")+1)
	scanner := bufio.NewScanner(strings.NewReader(listOutput))
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) >= 2 {
			commitShas = append(commitShas, parts[1])
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(commitShas) == 0 {
		return []string{}, nil
	}

	args := append([]string{"show", "-s", "--format=%H %ct"}, commitShas...)
	timestampOutput, _, err := executeGitCommand(args...)
	if err != nil {
		return nil, err
	}

	commitsWithNotes := make([]commitInfo, 0, len(commitShas))
	timestampScanner := bufio.NewScanner(strings.NewReader(timestampOutput))
	for timestampScanner.Scan() {
		parts := strings.Fields(timestampScanner.Text())
		if len(parts) >= 2 {
			ts, parseErr := strconv.ParseInt(parts[1], 10, 64)
			if parseErr != nil {
				return nil, parseErr
			}
			commitsWithNotes = append(commitsWithNotes, commitInfo{Sha: parts[0], Timestamp: ts})
		}
	}

	sort.Slice(commitsWithNotes, func(i, j int) bool {
		return commitsWithNotes[i].Timestamp > commitsWithNotes[j].Timestamp
	})
	out := make([]string, len(commitsWithNotes))
	for i, ci := range commitsWithNotes {
		out[i] = ci.Sha
	}
	return out, nil
}

func BenchmarkGitNotesCall_GetRef(b *testing.B) {
	manager, _, _ := benchmarkManagerForCalls(b, "bench-get-ref")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = manager.GetRef()
	}
}

func BenchmarkGitNotesCall_GetNote(b *testing.B) {
	benchmarkWithCommandMetrics(b, func(b *testing.B) {
		manager, sha, _ := benchmarkManagerForCalls(b, "bench-get-note")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := manager.GetNote(sha); err != nil {
				b.Fatalf("GetNote failed: %v", err)
			}
		}
	})
}

func BenchmarkGitNotesCall_GetNoteWithContext(b *testing.B) {
	manager, sha, _ := benchmarkManagerForCalls(b, "bench-get-note-with-context")
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := manager.GetNoteWithContext(ctx, sha); err != nil {
			b.Fatalf("GetNoteWithContext failed: %v", err)
		}
	}
}

func BenchmarkGitNotesCall_SetNote(b *testing.B) {
	manager, sha, _ := benchmarkManagerForCalls(b, "bench-set-note")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := manager.SetNote(sha, fmt.Sprintf("note-%d", i)); err != nil {
			b.Fatalf("SetNote failed: %v", err)
		}
	}
}

func BenchmarkGitNotesCall_GetNoteList(b *testing.B) {
	manager, _, _ := benchmarkManagerForCalls(b, "bench-get-note-list")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := manager.GetNoteList(); err != nil {
			b.Fatalf("GetNoteList failed: %v", err)
		}
	}
}

func BenchmarkGitNotesCall_GetNoteList_10(b *testing.B) {
	manager, _ := benchmarkManagerWithCountedNotes(b, "bench-get-note-list-10", 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := manager.GetNoteList(); err != nil {
			b.Fatalf("GetNoteList failed: %v", err)
		}
	}
}

func BenchmarkGitNotesCall_GetNoteList_100(b *testing.B) {
	manager, _ := benchmarkManagerWithCountedNotes(b, "bench-get-note-list-100", 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := manager.GetNoteList(); err != nil {
			b.Fatalf("GetNoteList failed: %v", err)
		}
	}
}

func BenchmarkGitNotesCall_GetNotesBulk(b *testing.B) {
	benchmarkWithCommandMetrics(b, func(b *testing.B) {
		manager, sha1, sha2 := benchmarkManagerForCalls(b, "bench-get-notes-bulk")
		shas := []string{sha1, sha2}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			results, errs := manager.GetNotesBulk(shas)
			if len(errs) > 0 {
				b.Fatalf("GetNotesBulk errors: %v", errs)
			}
			if len(results) != len(shas) {
				b.Fatalf("GetNotesBulk expected %d results, got %d", len(shas), len(results))
			}
		}
	})
}

func BenchmarkGitNotesCall_GetNotesBulk_10(b *testing.B) {
	manager, shas := benchmarkManagerWithCountedNotes(b, "bench-get-notes-bulk-10", 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, errs := manager.GetNotesBulk(shas)
		if len(errs) > 0 {
			b.Fatalf("GetNotesBulk errors: %v", errs)
		}
		if len(results) != len(shas) {
			b.Fatalf("GetNotesBulk expected %d results, got %d", len(shas), len(results))
		}
	}
}

func BenchmarkGitNotesCall_GetNotesBulk_50(b *testing.B) {
	manager, shas := benchmarkManagerWithCountedNotes(b, "bench-get-notes-bulk-50", 50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, errs := manager.GetNotesBulk(shas)
		if len(errs) > 0 {
			b.Fatalf("GetNotesBulk errors: %v", errs)
		}
		if len(results) != len(shas) {
			b.Fatalf("GetNotesBulk expected %d results, got %d", len(shas), len(results))
		}
	}
}

func BenchmarkGitNotesCompare_GetNotesBulk_Current_50(b *testing.B) {
	managerIface, shas := benchmarkManagerWithCountedNotes(b, "bench-compare-bulk-current-50", 50)
	manager := managerIface.(*notesManager)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, errs := manager.GetNotesBulk(shas)
		if len(errs) > 0 {
			b.Fatalf("GetNotesBulk current errors: %v", errs)
		}
		if len(results) != len(shas) {
			b.Fatalf("GetNotesBulk current expected %d results, got %d", len(shas), len(results))
		}
	}
}

func BenchmarkGitNotesCompare_GetNotesBulk_Legacy_50(b *testing.B) {
	managerIface, shas := benchmarkManagerWithCountedNotes(b, "bench-compare-bulk-legacy-50", 50)
	manager := managerIface.(*notesManager)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, errs := legacyGetNotesBulk(manager, shas)
		if len(errs) > 0 {
			b.Fatalf("GetNotesBulk legacy errors: %v", errs)
		}
		if len(results) != len(shas) {
			b.Fatalf("GetNotesBulk legacy expected %d results, got %d", len(shas), len(results))
		}
	}
}

func BenchmarkGitNotesCompare_GetNoteList_Current_100(b *testing.B) {
	managerIface, _ := benchmarkManagerWithCountedNotes(b, "bench-compare-list-current-100", 100)
	manager := managerIface.(*notesManager)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := manager.GetNoteList(); err != nil {
			b.Fatalf("GetNoteList current failed: %v", err)
		}
	}
}

func BenchmarkGitNotesCompare_GetNoteList_Legacy_100(b *testing.B) {
	managerIface, _ := benchmarkManagerWithCountedNotes(b, "bench-compare-list-legacy-100", 100)
	manager := managerIface.(*notesManager)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := legacyGetNoteListUncached(manager); err != nil {
			b.Fatalf("GetNoteList legacy failed: %v", err)
		}
	}
}

func BenchmarkGitNotesCall_DeleteNote(b *testing.B) {
	manager, sha, _ := benchmarkManagerForCalls(b, "bench-delete-note")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := manager.SetNote(sha, fmt.Sprintf("temp-note-%d", i)); err != nil {
			b.Fatalf("setup SetNote failed: %v", err)
		}
		if err := manager.DeleteNote(sha); err != nil {
			b.Fatalf("DeleteNote failed: %v", err)
		}
	}
}

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
