package main

import (
	"fmt"
	"time"

	"awesomeProject11/notes"
)

func main() {
	fmt.Println("Fetching notes... ")
	err := notes.FetchNotes("dd_notes", "origin")
	if err != nil {
		fmt.Println("Error fetching notes:", err)
	}

	defer func() {
		fmt.Println("Pushing notes... ")
		err := notes.PushNotes("dd_notes", "origin")
		if err != nil {
			fmt.Println("Error pushing notes:", err)
			return
		}
	}()

	fmt.Println("Shas with notes:")
	shas, err := notes.GetNoteList("dd_notes")
	if err != nil {
		fmt.Println("Error getting note list:", err)
		return
	}
	fmt.Println(shas)

	fmt.Println("Notes:")
	for _, sha := range shas {
		note, err := notes.GetNote("dd_notes", sha)
		if err != nil {
			fmt.Println("Error getting note:", err)
		}
		fmt.Println(sha, note)
	}

	fmt.Println("Notes with Bulk:")
	bnotes, berrors := notes.GetNotesBulk("dd_notes", shas)
	fmt.Println(bnotes)
	fmt.Println(berrors)

	fmt.Println("Note content from HEAD:")
	note, err := notes.GetNote("dd_notes", "")
	if err != nil {
		fmt.Println("Error getting note:", err)
	}
	fmt.Println(note)

	fmt.Println("Setting a new note...")
	err = notes.SetNote("dd_notes", "", "This is a new note from the future: "+time.Now().String())
	if err != nil {
		fmt.Println("Error setting note:", err)
	}
}
