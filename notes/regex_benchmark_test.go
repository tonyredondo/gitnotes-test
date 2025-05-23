package notes

import (
	"regexp"
	"strings"
	"testing"
)

// Benchmark comparing old string-based approach vs new regex-based approach

// Old approach using multiple string.Contains operations
func isNoteNotFoundErrorOld(errStr, stderr string) bool {
	stderrLower := strings.ToLower(stderr)
	return strings.Contains(errStr, "exit code 1") &&
		(strings.Contains(stderrLower, "no note found") ||
			strings.Contains(stderrLower, "no notes found"))
}

// Old approach for remote ref errors
func isRemoteRefNotFoundErrorOld(stderr, errStr string) bool {
	stderrLower := strings.ToLower(stderr)
	return strings.Contains(stderrLower, "couldn't find remote ref") ||
		strings.Contains(stderrLower, "no such ref") ||
		strings.Contains(stderrLower, "fetch-pack: invalid refspec") ||
		(strings.Contains(errStr, "exit status 1") && stderr == "") ||
		strings.Contains(errStr, "exit status 128")
}

// Old approach for SHA validation
func validateCommitSHAOld(sha string) bool {
	if sha == "" {
		return true
	}

	if strings.HasPrefix(sha, "-") || strings.Contains(sha, "..") {
		return false
	}

	if len(sha) < 4 || len(sha) > 40 {
		return false
	}

	for _, c := range sha {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}

	return true
}

// Test data
var (
	testErrorStr     = "git notes show failed with exit code 1: command failed"
	testStderr       = "error: no note found for object abc123def456"
	testRemoteStderr = "error: couldn't find remote ref refs/notes/test"
	testRemoteErrStr = "git fetch failed with exit status 128"
	testValidSHA     = "abc123def456789012345678901234567890abcd"
	testInvalidSHA   = "-invalid..sha"
)

// Benchmarks for note not found error detection
func BenchmarkNoteNotFoundErrorOld(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = isNoteNotFoundErrorOld(testErrorStr, testStderr)
	}
}

func BenchmarkNoteNotFoundErrorNew(b *testing.B) {
	matcher := NewErrorMatcher()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = matcher.IsNoteNotFoundError(testErrorStr, testStderr)
	}
}

// Benchmarks for remote ref error detection
func BenchmarkRemoteRefErrorOld(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = isRemoteRefNotFoundErrorOld(testRemoteStderr, testRemoteErrStr)
	}
}

func BenchmarkRemoteRefErrorNew(b *testing.B) {
	matcher := NewErrorMatcher()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = matcher.IsRemoteRefNotFoundError(testRemoteStderr, testRemoteErrStr)
	}
}

// Benchmarks for SHA validation
func BenchmarkValidateSHAOld(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = validateCommitSHAOld(testValidSHA)
		_ = validateCommitSHAOld(testInvalidSHA)
	}
}

func BenchmarkValidateSHANew(b *testing.B) {
	matcher := NewErrorMatcher()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = matcher.ValidateCommitSHA(testValidSHA)
		_ = matcher.ValidateCommitSHA(testInvalidSHA)
	}
}

// Benchmark regex compilation cost (should be done once)
func BenchmarkRegexCompilation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = regexp.MustCompile(`(?i)no notes? found`)
		_ = regexp.MustCompile(`exit code 1`)
		_ = regexp.MustCompile(`(?i)couldn't find remote ref`)
	}
}

// Benchmark using pre-compiled vs on-demand compilation
func BenchmarkPreCompiledRegex(b *testing.B) {
	pattern := regexp.MustCompile(`(?i)no notes? found`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pattern.MatchString(testStderr)
	}
}

func BenchmarkOnDemandRegex(b *testing.B) {
	for i := 0; i < b.N; i++ {
		pattern := regexp.MustCompile(`(?i)no notes? found`)
		_ = pattern.MatchString(testStderr)
	}
}

// Benchmark memory allocations
func BenchmarkStringToLowerAllocations(b *testing.B) {
	testStr := "ERROR: COULDN'T FIND REMOTE REF REFS/NOTES/TEST"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lower := strings.ToLower(testStr)
		_ = strings.Contains(lower, "couldn't find remote ref")
	}
}

func BenchmarkRegexNoAllocations(b *testing.B) {
	testStr := "ERROR: COULDN'T FIND REMOTE REF REFS/NOTES/TEST"
	pattern := regexp.MustCompile(`(?i)couldn't find remote ref`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pattern.MatchString(testStr)
	}
}
