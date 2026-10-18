package build

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/dataview"
	"github.com/deweysasser/obsidian-publish-website/internal/index"
	"github.com/deweysasser/obsidian-publish-website/internal/mdscan"
	"github.com/deweysasser/obsidian-publish-website/internal/vault"
)

// TestSmokeRealVault evaluates every dataview block in a real vault through
// the bridge. Gated: OBSIDIAN_PUBLISH_SMOKE_VAULT=/path/to/vault.
// Query errors are logged (vaults can contain broken queries); a bridge
// failure fails the test.
func TestSmokeRealVault(t *testing.T) {
	root := os.Getenv("OBSIDIAN_PUBLISH_SMOKE_VAULT")
	if root == "" {
		t.Skip("set OBSIDIAN_PUBLISH_SMOKE_VAULT to run")
	}
	node, err := dataview.FindNode()
	if err != nil {
		t.Skip(err)
	}
	v, err := vault.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	ix := index.Build(v)
	var reqs []dataview.Request
	for _, p := range ix.Paths {
		n := ix.Pages[p].Note
		lines := mdscan.Lines(n.Body, n.BodyLine)
		for i := 0; i < len(lines); i++ {
			if !lines[i].FenceOpen || lines[i].Lang != "dataview" {
				continue
			}
			var code []string
			j := i + 1
			for ; j < len(lines) && !lines[j].FenceClose; j++ {
				code = append(code, strings.TrimPrefix(lines[j].Text, lines[i].Quote))
			}
			reqs = append(reqs, dataview.Request{ID: p + ":" + itoa(lines[i].Num), Kind: "query", Origin: p, Source: strings.Join(code, "\n")})
			i = j
		}
	}
	settings, _ := dataview.LoadSettings(root)
	res, err := dataview.NewEngine(node, settings, ix, nil).RunIndex(reqs)
	if err != nil {
		t.Fatal(err)
	}
	failed := 0
	for _, r := range reqs {
		if got := res[r.ID]; !got.OK && !got.Unsupported {
			failed++
			t.Logf("%s: %s", r.ID, firstLine(got.Error))
		}
	}
	t.Logf("%d notes, %d queries, %d failed", len(ix.Paths), len(reqs), failed)
}

func itoa(n int) string { return strconv.Itoa(n) }

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}
