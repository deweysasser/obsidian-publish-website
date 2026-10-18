// Package transform is the post-render hook stage (e.g. future redaction of
// addresses or phone numbers). Transforms run before any target writes, so
// every output format and the search index see transformed content.
package transform

import (
	"fmt"
	"sort"
	"sync"

	"github.com/deweysasser/obsidian-publish-website/internal/render"
)

// Transform rewrites a rendered page. Implementations should operate on text
// content (Title, HTML text nodes, Markdown) without corrupting markup.
type Transform interface {
	Name() string
	Apply(p *render.Page) error
}

var (
	mu       sync.Mutex
	registry = map[string]func() Transform{}
)

// Register makes a transform available to config by name.
func Register(name string, f func() Transform) {
	mu.Lock()
	defer mu.Unlock()
	registry[name] = f
}

// Chain instantiates the named transforms in order.
func Chain(names []string) ([]Transform, error) {
	mu.Lock()
	defer mu.Unlock()
	var out []Transform
	for _, n := range names {
		f, ok := registry[n]
		if !ok {
			known := make([]string, 0, len(registry))
			for k := range registry {
				known = append(known, k)
			}
			sort.Strings(known)
			return nil, fmt.Errorf("unknown transform %q (available: %v)", n, known)
		}
		out = append(out, f())
	}
	return out, nil
}

// Apply runs every transform on p.
func Apply(ts []Transform, p *render.Page) error {
	for _, t := range ts {
		if err := t.Apply(p); err != nil {
			return fmt.Errorf("transform %s on %s: %w", t.Name(), p.SourcePath, err)
		}
	}
	return nil
}
