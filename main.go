package main

import (
	"fmt"
	"time"

	"awesomeProject11/notes"
)

func main() {

	fmt.Println("Creating manager... ")
	manager := notes.NewTimedNotesManager(notes.NewNotesManager("dd_notes"))

	fmt.Println("Fetching notes... ")
	err := manager.FetchNotes("origin")
	if err != nil {
		fmt.Println("Error fetching notes:", err)
	}

	defer func() {
		fmt.Println("Pushing notes... ")
		err := manager.PushNotes("origin")
		if err != nil {
			fmt.Println("Error pushing notes:", err)
			return
		}
	}()

	fmt.Println("Shas with notes:")
	shas, err := manager.GetNoteList()
	if err != nil {
		fmt.Println("Error getting note list:", err)
		return
	}
	fmt.Println(shas)

	fmt.Println("Notes:")
	for _, sha := range shas {
		note, err := manager.GetNote(sha)
		if err != nil {
			fmt.Println("Error getting note:", err)
		}
		fmt.Println(sha, note)
	}

	fmt.Println("Notes with Bulk:")
	bnotes, berrors := manager.GetNotesBulk(shas)
	fmt.Println(bnotes)
	fmt.Println(berrors)

	fmt.Println("Note content from HEAD:")
	note, err := manager.GetNote("")
	if err != nil {
		fmt.Println("Error getting note:", err)
	}
	fmt.Println(note)

	fmt.Println("Setting a new note...")
	err = manager.SetNote("", "This is a new note from the future: "+time.Now().String())
	if err != nil {
		fmt.Println("Error setting note:", err)
	}

	fmt.Println("Creating manager for JSON and fetching...")
	jsonManager := notes.NewTimedNotesManager(notes.NewNotesManager("dd_notes_json"))
	_ = jsonManager.FetchNotes("origin")
	defer func() {
		_ = jsonManager.PushNotes("origin")
	}()

	m := map[string]string{
		"test":  "test",
		"test2": "test2",
	}
	fmt.Println("Setting a new note with JSON...")
	err = notes.SetNoteJSON(jsonManager, "", m)
	if err != nil {
		fmt.Println("Error setting note:", err)
	}
	fmt.Println("Getting note with JSON...")
	jsonNotes, err := notes.GetNoteJSON[map[string]string](jsonManager, "")
	if err != nil {
		fmt.Println("Error getting note:", err)
	}
	fmt.Println(jsonNotes)
}
