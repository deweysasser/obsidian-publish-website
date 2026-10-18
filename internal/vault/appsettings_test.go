package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStrictLineBreaks(t *testing.T) {
	dir := t.TempDir()
	if StrictLineBreaks(dir) {
		t.Error("missing app.json should mean Obsidian's default (false)")
	}
	os.MkdirAll(filepath.Join(dir, ".obsidian"), 0o755)
	os.WriteFile(filepath.Join(dir, ".obsidian", "app.json"), []byte(`{"strictLineBreaks": true}`), 0o644)
	if !StrictLineBreaks(dir) {
		t.Error("strictLineBreaks: true not honored")
	}
}
