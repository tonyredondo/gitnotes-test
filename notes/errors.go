package notes

import (
	"errors"
	"fmt"
)

// NoteNotFoundError is returned when a note does not exist for the given commit SHA.
type NoteNotFoundError struct {
	Ref       string
	CommitSha string
}

func (e *NoteNotFoundError) Error() string {
	return "note not found for commit " + e.CommitSha + " in ref " + e.Ref
}

func IsNoteNotFound(err error) bool {
	var nf *NoteNotFoundError
	return errors.As(err, &nf)
}

// InvalidCommitShaError is returned when the commit SHA does not exist or is invalid.
type InvalidCommitShaError struct {
	CommitSha string
}

func (e *InvalidCommitShaError) Error() string {
	return "invalid or non-existent commit SHA: " + e.CommitSha
}

func IsInvalidCommitSha(err error) bool {
	var nf *InvalidCommitShaError
	return errors.As(err, &nf)
}

// NoteSizeExceededError is returned when a note exceeds the maximum allowed size.
type NoteSizeExceededError struct {
	Size    int
	MaxSize int
}

func (e *NoteSizeExceededError) Error() string {
	return fmt.Sprintf("note size %d exceeds maximum allowed size %d", e.Size, e.MaxSize)
}

func IsNoteSizeExceededError(err error) bool {
	var nf *NoteSizeExceededError
	return errors.As(err, &nf)
}
