package notes

import "regexp"

// Pre-compiled regex patterns for optimized string processing
var (
	// Error detection patterns
	// Note-related error patterns (case-insensitive)
	noteNotFoundPattern    = regexp.MustCompile(`(?i)no notes? found`)
	noteNotFoundObjPattern = regexp.MustCompile(`(?i)no note found for object`)
	objectHasNoNotePattern = regexp.MustCompile(`(?i)object has no note`)

	// Git resolution errors
	fatalFailedResolvePattern = regexp.MustCompile(`(?i)fatal: failed to resolve`)

	// Remote operation patterns (case-insensitive)
	remoteRefNotFoundPattern = regexp.MustCompile(`(?i)couldn't find remote ref`)
	noSuchRefPattern         = regexp.MustCompile(`(?i)no such ref`)
	invalidRefspecPattern    = regexp.MustCompile(`(?i)fetch-pack: invalid refspec`)
	remoteUnavailablePattern = regexp.MustCompile(`(?i)does not appear to be a git repository`)

	// Repository state patterns (case-insensitive)
	badNotesRefPattern        = regexp.MustCompile(`(?i)bad notes ref`)
	unknownNotesRefPattern    = regexp.MustCompile(`(?i)unknown notes ref`)
	cannotReadNotesRefPattern = regexp.MustCompile(`(?i)cannot read notes`)

	// Push/merge conflict patterns (case-insensitive)
	nonFastForwardPattern = regexp.MustCompile(`(?i)non-fast-forward`)
	fetchFirstPattern     = regexp.MustCompile(`(?i)fetch first`)
	rejectedPattern       = regexp.MustCompile(`(?i)rejected`)
	conflictPattern       = regexp.MustCompile(`(?i)conflict`)

	// Merge status patterns (case-insensitive)
	alreadyUpToDatePattern = regexp.MustCompile(`(?i)already up to date`)
	nothingToMergePattern  = regexp.MustCompile(`(?i)nothing to merge`)

	// SHA validation patterns
	invalidShaPattern = regexp.MustCompile(`^-|\.\.`)
	hexCharPattern    = regexp.MustCompile(`^[0-9a-fA-F]+$`)
)

// ErrorMatcher provides optimized error pattern matching
type ErrorMatcher struct{}

// NewErrorMatcher creates a new error matcher instance
func NewErrorMatcher() *ErrorMatcher {
	return &ErrorMatcher{}
}

// IsNoteNotFoundError checks if the error indicates a note was not found
func (em *ErrorMatcher) IsNoteNotFoundError(exitCode int, stderr string) bool {
	return exitCode == 1 &&
		(noteNotFoundPattern.MatchString(stderr) ||
			noteNotFoundObjPattern.MatchString(stderr))
}

// IsInvalidCommitError checks if the error indicates an invalid commit SHA
func (em *ErrorMatcher) IsInvalidCommitError(exitCode int, stderr string) bool {
	return fatalFailedResolvePattern.MatchString(stderr) || exitCode == 128
}

// IsRemoteRefNotFoundError checks if the error indicates remote ref doesn't exist
func (em *ErrorMatcher) IsRemoteRefNotFoundError(exitCode int, stderr string) bool {
	return (remoteRefNotFoundPattern.MatchString(stderr) ||
		noSuchRefPattern.MatchString(stderr) ||
		invalidRefspecPattern.MatchString(stderr)) &&
		(exitCode == 1 || exitCode == 128)
}

// IsRemoteUnavailableError checks if the configured remote itself is unavailable.
func (em *ErrorMatcher) IsRemoteUnavailableError(stderr string) bool {
	return remoteUnavailablePattern.MatchString(stderr)
}

// IsNotesRefNotFoundError checks if notes reference doesn't exist
func (em *ErrorMatcher) IsNotesRefNotFoundError(exitCode int, stderr string) bool {
	return (badNotesRefPattern.MatchString(stderr) ||
		unknownNotesRefPattern.MatchString(stderr) ||
		cannotReadNotesRefPattern.MatchString(stderr) ||
		noteNotFoundPattern.MatchString(stderr)) && exitCode == 1
}

// IsDeleteNoteNotFoundError checks if delete failed because note doesn't exist
func (em *ErrorMatcher) IsDeleteNoteNotFoundError(exitCode int, stderr string) bool {
	return objectHasNoNotePattern.MatchString(stderr) && exitCode == 1
}

// IsPushRetryableError checks if push error is retryable (due to concurrent changes)
func (em *ErrorMatcher) IsPushRetryableError(stderr string) bool {
	return nonFastForwardPattern.MatchString(stderr) ||
		fetchFirstPattern.MatchString(stderr) ||
		rejectedPattern.MatchString(stderr)
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
	if sha == "" {
		return true // Allow empty, will use HEAD
	}

	// Check for invalid patterns
	if invalidShaPattern.MatchString(sha) {
		return false
	}

	// Validate length and hex format
	if len(sha) < 4 || len(sha) > 40 {
		return false
	}

	return hexCharPattern.MatchString(sha)
}
