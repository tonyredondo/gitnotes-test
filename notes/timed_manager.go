package notes

import (
	"context"
	"fmt"
	"time"
)

type timedNotesManager struct {
	NotesManager
}

// NewTimedNotesManager creates a new notes manager with timing capabilities
func NewTimedNotesManager(manager NotesManager) NotesManager {
	return &timedNotesManager{NotesManager: manager}
}

func (m *timedNotesManager) GetRef() string {
	t := time.Now()
	defer func() {
		fmt.Println("\ttimedNotesManager.GetRef() took", time.Since(t))
	}()
	return m.NotesManager.GetRef()
}

func (m *timedNotesManager) GetNote(commitSha string) (string, error) {
	t := time.Now()
	defer func() {
		fmt.Println("\ttimedNotesManager.GetNote() took", time.Since(t))
	}()
	return m.NotesManager.GetNote(commitSha)
}

func (m *timedNotesManager) GetNoteWithContext(ctx context.Context, commitSha string) (string, error) {
	t := time.Now()
	defer func() {
		fmt.Println("\ttimedNotesManager.GetNoteWithContext() took", time.Since(t))
	}()
	return m.NotesManager.GetNoteWithContext(ctx, commitSha)
}

func (m *timedNotesManager) GetNotesBulk(commitShas []string) (map[string]string, map[string]error) {
	t := time.Now()
	defer func() {
		fmt.Println("\ttimedNotesManager.GetNotesBulk() took", time.Since(t))
	}()
	return m.NotesManager.GetNotesBulk(commitShas)
}

func (m *timedNotesManager) SetNote(commitSha, value string) error {
	t := time.Now()
	defer func() {
		fmt.Println("\ttimedNotesManager.SetNote() took", time.Since(t))
	}()
	return m.NotesManager.SetNote(commitSha, value)
}

func (m *timedNotesManager) GetNoteList() ([]string, error) {
	t := time.Now()
	defer func() {
		fmt.Println("\ttimedNotesManager.GetNoteList() took", time.Since(t))
	}()
	return m.NotesManager.GetNoteList()
}

func (m *timedNotesManager) DeleteNote(commitSha string) error {
	t := time.Now()
	defer func() {
		fmt.Println("\ttimedNotesManager.DeleteNote() took", time.Since(t))
	}()
	return m.NotesManager.DeleteNote(commitSha)
}

func (m *timedNotesManager) FetchNotes(remoteName string) error {
	t := time.Now()
	defer func() {
		fmt.Println("\ttimedNotesManager.FetchNotes() took", time.Since(t))
	}()
	return m.NotesManager.FetchNotes(remoteName)
}

func (m *timedNotesManager) PushNotes(remoteName string) error {
	t := time.Now()
	defer func() {
		fmt.Println("\ttimedNotesManager.PushNotes() took", time.Since(t))
	}()
	return m.NotesManager.PushNotes(remoteName)
}

func (m *timedNotesManager) PushNotesWithRetry(remoteName string, maxRetries int) error {
	t := time.Now()
	defer func() {
		fmt.Println("\ttimedNotesManager.PushNotesWithRetry() took", time.Since(t))
	}()
	return m.NotesManager.PushNotesWithRetry(remoteName, maxRetries)
}
