package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("Fetching notes... ")
	err := FetchNotes("dd_notes", "origin")
	if err != nil {
		fmt.Println("Error fetching notes:", err)
		return
	}

	defer func() {
		fmt.Println("Pushing notes... ")
		err := PushNotes("dd_notes", "origin")
		if err != nil {
			fmt.Println("Error pushing notes:", err)
			return
		}
	}()

	fmt.Println("Shas with notes:")
	shas, err := GetNoteList("dd_notes")
	if err != nil {
		fmt.Println("Error getting note list:", err)
		return
	}
	fmt.Println(shas)

	fmt.Println("Notes:")
	for _, sha := range shas {
		note, err := GetNote("dd_notes", sha)
		if err != nil {
			fmt.Println("Error getting note:", err)
			return
		}
		fmt.Println(sha, note)
	}

	fmt.Println("Note content from HEAD:")
	note, err := GetNote("dd_notes", "")
	if err != nil {
		fmt.Println("Error getting note:", err)
		return
	}
	fmt.Println(note)

	fmt.Println("Setting a new note...")
	err = SetNote("dd_notes", "", "This is a new note from the future: "+time.Now().String())
	if err != nil {
		fmt.Println("Error setting note:", err)
		return
	}
}
