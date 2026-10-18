package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// StrictLineBreaks reports Obsidian's "Strict line breaks" editor setting
// from .obsidian/app.json. Missing or unreadable settings mean Obsidian's
// default, false.
func StrictLineBreaks(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, ".obsidian", "app.json"))
	if err != nil {
		return false
	}
	var s struct {
		StrictLineBreaks bool `json:"strictLineBreaks"`
	}
	_ = json.Unmarshal(data, &s)
	return s.StrictLineBreaks
}
