package mdscan

import "strings"

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
