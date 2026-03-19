package notes

import "testing"

func TestIsRemoteRefNotFoundErrorAvoidsFalsePositives(t *testing.T) {
	matcher := NewErrorMatcher()

	if matcher.IsRemoteRefNotFoundError(128, "") {
		t.Fatal("expected generic exit status 128 to not be classified as remote ref not found")
	}

	if !matcher.IsRemoteRefNotFoundError(128, "fatal: couldn't find remote ref refs/notes/test") {
		t.Fatal("expected explicit remote ref missing message to be classified as remote ref not found")
	}
}

func TestIsNotesRefNotFoundErrorAvoidsBroadMatching(t *testing.T) {
	matcher := NewErrorMatcher()

	if matcher.IsNotesRefNotFoundError(1, "fatal: path does not exist") {
		t.Fatal("expected generic does-not-exist message to not be treated as notes ref not found")
	}

	if !matcher.IsNotesRefNotFoundError(1, "error: bad notes ref refs/notes/missing") {
		t.Fatal("expected bad notes ref error to be recognized")
	}
}
