package target

import (
	"testing"
	"time"
)

func TestExportFrontmatterTimestamps(t *testing.T) {
	est := time.FixedZone("EST", -5*3600)
	got := ExportFrontmatter(map[string]any{
		"date":   time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC),
		"zoned":  time.Date(2026, 9, 22, 0, 0, 0, 0, est),
		"timed":  time.Date(2026, 9, 22, 10, 30, 0, 0, time.UTC),
		"nested": map[string]any{"l": []any{time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}},
	})
	if got["date"] != "2026-09-22" || got["zoned"] != "2026-09-22T00:00:00-05:00" || got["timed"] != "2026-09-22T10:30:00Z" {
		t.Errorf("%v", got)
	}
	if got["nested"].(map[string]any)["l"].([]any)[0] != "2026-01-02" {
		t.Errorf("nested %v", got["nested"])
	}
}
