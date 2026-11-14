package search

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

// Pagefind numbers pages in the order they are added, and every index chunk
// stores page numbers. Indexing in path order means a single new page
// renumbers everything after it and renames nearly every chunk, so an
// incremental deploy re-uploads the whole index. Keeping each page's
// position from the previous build limits a deploy to the chunks holding
// the words that actually changed.

// orderState is the page order of the last successful index, kept in the
// user cache (never in the vault or the published site). Losing it costs
// one full renumbering.
type orderState struct {
	Version int      `json:"version"`
	Pages   []string `json:"pages"`
}

const orderStateVersion = 1

// orderStats says how an order differs from the previous one.
type orderStats struct {
	New, Removed int
}

// stableOrder lists current (site-relative .html paths) keeping each page
// that prev also has at its previous position. New pages, in path order,
// first fill the gaps that removed pages left, then go at the end; gaps
// left over close up, shifting the pages after them. With no prev, the
// order is by path.
func stableOrder(prev, current []string) ([]string, orderStats) {
	have := make(map[string]bool, len(current))
	for _, p := range current {
		have[p] = true
	}
	placed := make(map[string]bool, len(current))
	order := make([]string, 0, len(current))
	var holes []int
	var st orderStats
	for _, p := range prev {
		switch {
		case placed[p]: // a duplicate in a damaged state file
		case have[p]:
			order = append(order, p)
			placed[p] = true
		default:
			holes = append(holes, len(order))
			order = append(order, "")
			st.Removed++
		}
	}
	var fresh []string
	for _, p := range current {
		if !placed[p] {
			fresh = append(fresh, p)
		}
	}
	sort.Strings(fresh)
	st.New = len(fresh)
	for len(holes) > 0 && len(fresh) > 0 {
		order[holes[0]] = fresh[0]
		holes, fresh = holes[1:], fresh[1:]
	}
	order = append(order, fresh...)
	if len(holes) > 0 {
		kept := order[:0]
		for _, p := range order {
			if p != "" {
				kept = append(kept, p)
			}
		}
		order = kept
	}
	return order, st
}

// loadOrder reads the previous order; a missing or unreadable file, or one
// from another version, is an empty order.
func loadOrder(file string) []string {
	if file == "" {
		return nil
	}
	b, err := os.ReadFile(file) // #nosec G304 -- our own cache file
	if err != nil {
		return nil
	}
	var st orderState
	if json.Unmarshal(b, &st) != nil || st.Version != orderStateVersion {
		return nil
	}
	return st.Pages
}

// saveOrder writes the order atomically, so an interrupted build never
// leaves a truncated file.
func saveOrder(file string, pages []string) error {
	if file == "" {
		return errors.New("no state file")
	}
	b, err := json.Marshal(orderState{Version: orderStateVersion, Pages: pages})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".order-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close() // the write error is the one to report
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}
