package mdscan

import (
	"strings"
	"unicode/utf8"
)

// StripComments removes %%...%% comments (inline and multi-line) from prose.
// Matches Obsidian reading view: code spans are literal.
func StripComments(lines []Line) []Line {
	var out []Line
	in := false
	for _, ln := range lines {
		if ln.InFence || ln.FenceOpen || ln.FenceClose {
			if !in {
				out = append(out, ln)
			}
			continue
		}
		if !in && !strings.Contains(ln.Text, "%%") {
			out = append(out, ln)
			continue
		}
		var b strings.Builder
		touched := in
		for _, seg := range Inline(ln.Text) {
			if seg.Code && !seg.Math {
				// Code spans are literal, as in Obsidian: %% inside them neither
				// opens nor closes a comment.
				if !in {
					b.WriteString(seg.Text)
				}
				continue
			}
			// Prose and math: %% is a comment delimiter (it is never meaningful
			// LaTeX, and $...$ detection is heuristic, so fail closed).
			parts := strings.Split(seg.Text, "%%")
			for k, p := range parts {
				if k > 0 {
					in = !in
					touched = true
				}
				if !in {
					b.WriteString(p)
				}
			}
		}
		if touched && strings.TrimSpace(b.String()) == "" {
			continue
		}
		ln.Text = b.String()
		out = append(out, ln)
	}
	return out
}

// BlankComments returns text with CRLF normalized to LF and every %%comment%%
// (markers included) replaced by spaces, one per rune, so line numbers and
// columns are unchanged. Code is left alone, matching StripComments.
func BlankComments(raw []byte) string {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	lines := Lines([]byte(text), 1)
	out := make([]string, len(lines))
	in := false
	blank := func(s string) string { return strings.Repeat(" ", utf8.RuneCountInString(s)) }
	for i, ln := range lines {
		if ln.InFence || ln.FenceOpen || ln.FenceClose {
			if in {
				out[i] = blank(ln.Text)
			} else {
				out[i] = ln.Text
			}
			continue
		}
		var b strings.Builder
		for _, seg := range Inline(ln.Text) {
			if seg.Code && !seg.Math {
				if in {
					b.WriteString(blank(seg.Text))
				} else {
					b.WriteString(seg.Text)
				}
				continue
			}
			parts := strings.Split(seg.Text, "%%")
			for k, p := range parts {
				if k > 0 {
					b.WriteString("  ")
					in = !in
				}
				if in {
					b.WriteString(blank(p))
				} else {
					b.WriteString(p)
				}
			}
		}
		out[i] = b.String()
	}
	s := strings.Join(out, "\n")
	if strings.HasSuffix(text, "\n") {
		s += "\n"
	}
	return s
}
