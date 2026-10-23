// Package vault loads every note and attachment in an Obsidian vault.
package vault

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Note is a single markdown file in the vault.
type Note struct {
	Path        string // vault-relative, slash-separated, with extension
	Raw         []byte
	Body        []byte // Raw minus the frontmatter block
	BodyLine    int    // 1-based line number in Raw where Body starts
	Frontmatter map[string]any
	Ctime       time.Time
	Mtime       time.Time
	Size        int64
}

// Vault is the loaded content of a vault directory.
type Vault struct {
	Root        string
	Notes       []*Note
	Attachments []string
	Warnings    []string

	byPath map[string]*Note
}

// Load walks root, reading all .md notes and listing all other files.
// Directories and files whose names start with "." are skipped, as are
// Syncthing conflict copies (name.sync-conflict-<date>-<time>-<device>.ext).
func Load(root string) (*Vault, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	v := &Vault{Root: abs, byPath: map[string]*Note{}}
	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == abs {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || strings.Contains(d.Name(), ".sync-conflict-") {
			return nil
		}
		rel, err := filepath.Rel(abs, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !d.Type().IsRegular() {
			// Symlinks could point outside the vault; never publish through them.
			v.Warnings = append(v.Warnings, fmt.Sprintf("%s: skipped (not a regular file)", rel))
			return nil
		}
		if !strings.EqualFold(path.Ext(rel), ".md") {
			v.Attachments = append(v.Attachments, rel)
			return nil
		}
		n, warn, err := readNote(p, rel)
		if err != nil {
			return err
		}
		if warn != "" {
			v.Warnings = append(v.Warnings, warn)
		}
		v.Notes = append(v.Notes, n)
		v.byPath[rel] = n
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(v.Notes, func(i, j int) bool { return v.Notes[i].Path < v.Notes[j].Path })
	sort.Strings(v.Attachments)
	return v, nil
}

// Note returns the note at a vault-relative path, or nil.
func (v *Vault) Note(p string) *Note { return v.byPath[p] }

// AbsPath returns the filesystem path of a vault-relative path.
func (v *Vault) AbsPath(rel string) string { return filepath.Join(v.Root, filepath.FromSlash(rel)) }

func readNote(abs, rel string) (*Note, string, error) {
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, "", err
	}
	n := &Note{Path: rel, Raw: raw, Mtime: st.ModTime(), Size: st.Size()}
	n.Ctime = birthTime(abs, st)
	fm, body, line := SplitFrontmatter(raw)
	n.Body, n.BodyLine = body, line
	var warn string
	if fm != nil {
		n.Frontmatter = map[string]any{}
		if err := yaml.Unmarshal(fm, &n.Frontmatter); err != nil {
			n.Frontmatter = map[string]any{}
			warn = fmt.Sprintf("%s:2: invalid frontmatter: %v", rel, err)
		}
		if n.Frontmatter == nil {
			n.Frontmatter = map[string]any{}
		}
	}
	return n, warn, nil
}

// SplitFrontmatter separates a leading YAML frontmatter block from the body.
// fm is nil when there is no (terminated) frontmatter block.
func SplitFrontmatter(raw []byte) (fm []byte, body []byte, bodyLine int) {
	first, rest, ok := cutLine(raw)
	if !ok || strings.TrimRight(string(first), "\r") != "---" {
		return nil, raw, 1
	}
	start := len(raw) - len(rest)
	line := 2
	for pos := start; pos < len(raw); {
		l, after, _ := cutLine(raw[pos:])
		if strings.TrimRight(string(l), "\r") == "---" {
			return raw[start:pos], after, line + 1
		}
		line++
		pos = len(raw) - len(after)
		if len(after) == 0 {
			break
		}
	}
	return nil, raw, 1
}

// cutLine splits b at the first newline; ok reports whether a newline was found
// or b is non-empty.
func cutLine(b []byte) (line, rest []byte, ok bool) {
	if len(b) == 0 {
		return nil, nil, false
	}
	i := bytes.IndexByte(b, '\n')
	if i < 0 {
		return b, nil, true
	}
	return b[:i], b[i+1:], true
}
