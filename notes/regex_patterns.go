package notes

import "regexp"

// Pre-compiled regex patterns for optimized string processing
var (
	// Error detection patterns
	exitCode1Pattern     = regexp.MustCompile(`exit code 1`)
	exitCode128Pattern   = regexp.MustCompile(`exit code 128`)
	exitStatus1Pattern   = regexp.MustCompile(`exit status 1`)
	exitStatus128Pattern = regexp.MustCompile(`exit status 128`)

	// Note-related error patterns (case-insensitive)
	noteNotFoundPattern    = regexp.MustCompile(`(?i)no notes? found`)
	noteNotFoundObjPattern = regexp.MustCompile(`(?i)no note found for object`)
	objectHasNoNotePattern = regexp.MustCompile(`(?i)object has no note`)
	failedToGetNotePattern = regexp.MustCompile(`(?i)failed to get note`)

	// Git resolution errors
	fatalFailedResolvePattern = regexp.MustCompile(`(?i)fatal: failed to resolve`)

	// Remote operation patterns (case-insensitive)
	remoteRefNotFoundPattern = regexp.MustCompile(`(?i)couldn't find remote ref`)
	noSuchRefPattern         = regexp.MustCompile(`(?i)no such ref`)
	invalidRefspecPattern    = regexp.MustCompile(`(?i)fetch-pack: invalid refspec`)

	// Repository state patterns (case-insensitive)
	badNotesRefPattern  = regexp.MustCompile(`(?i)bad notes ref`)
	doesNotExistPattern = regexp.MustCompile(`(?i)does not exist`)

	// Push/merge conflict patterns (case-insensitive)
	nonFastForwardPattern = regexp.MustCompile(`(?i)non-fast-forward`)
	fetchFirstPattern     = regexp.MustCompile(`(?i)fetch first`)
	rejectedPattern       = regexp.MustCompile(`(?i)rejected`)
	conflictPattern       = regexp.MustCompile(`(?i)conflict`)

	// Merge status patterns (case-insensitive)
	alreadyUpToDatePattern = regexp.MustCompile(`(?i)already up to date`)
	nothingToMergePattern  = regexp.MustCompile(`(?i)nothing to merge`)
)

// ErrorMatcher provides optimized error pattern matching
type ErrorMatcher struct{}

// NewErrorMatcher creates a new error matcher instance
func NewErrorMatcher() *ErrorMatcher {
	return &ErrorMatcher{}
}

// IsNoteNotFoundError checks if the error indicates a note was not found
func (em *ErrorMatcher) IsNoteNotFoundError(errStr, stderr string) bool {
	return exitCode1Pattern.MatchString(errStr) &&
		(noteNotFoundPattern.MatchString(stderr) ||
			noteNotFoundObjPattern.MatchString(errStr) ||
			failedToGetNotePattern.MatchString(errStr))
}

// IsInvalidCommitError checks if the error indicates an invalid commit SHA
func (em *ErrorMatcher) IsInvalidCommitError(errStr string) bool {
	return fatalFailedResolvePattern.MatchString(errStr) ||
		exitCode128Pattern.MatchString(errStr)
}

// IsRemoteRefNotFoundError checks if the error indicates remote ref doesn't exist
func (em *ErrorMatcher) IsRemoteRefNotFoundError(stderr, errStr string) bool {
	return (remoteRefNotFoundPattern.MatchString(stderr) ||
		noSuchRefPattern.MatchString(stderr) ||
		invalidRefspecPattern.MatchString(stderr) ||
		(exitStatus1Pattern.MatchString(errStr) && stderr == "") ||
		exitStatus128Pattern.MatchString(errStr))
}

// IsNotesRefNotFoundError checks if notes reference doesn't exist
func (em *ErrorMatcher) IsNotesRefNotFoundError(errMsg string) bool {
	return badNotesRefPattern.MatchString(errMsg) ||
		doesNotExistPattern.MatchString(errMsg) ||
		noteNotFoundPattern.MatchString(errMsg)
}

// IsDeleteNoteNotFoundError checks if delete failed because note doesn't exist
func (em *ErrorMatcher) IsDeleteNoteNotFoundError(stderr, errStr string) bool {
	return objectHasNoNotePattern.MatchString(stderr) ||
		exitCode1Pattern.MatchString(errStr)
}

// IsPushRetryableError checks if push error is retryable (due to concurrent changes)
func (em *ErrorMatcher) IsPushRetryableError(errStr string) bool {
	return nonFastForwardPattern.MatchString(errStr) ||
		fetchFirstPattern.MatchString(errStr) ||
		rejectedPattern.MatchString(errStr)
}

// IsMergeUpToDate checks if merge indicates already up to date
func (em *ErrorMatcher) IsMergeUpToDate(mergeStderr string) bool {
	return alreadyUpToDatePattern.MatchString(mergeStderr) ||
		nothingToMergePattern.MatchString(mergeStderr)
}

// IsMergeConflict checks if merge failed due to conflict
func (em *ErrorMatcher) IsMergeConflict(mergeStderr string) bool {
	return conflictPattern.MatchString(mergeStderr)
}

// ValidateCommitSHA validates commit SHA format using regex
func (em *ErrorMatcher) ValidateCommitSHA(sha string) bool {
	if !isSafeRevisionSpec(sha) {
		return false
	}

	if sha == "" {
		return true // Allow empty, will use HEAD
	}

	_, _, err := executeGitCommand("rev-parse", "--verify", sha)
	return err == nil
}
