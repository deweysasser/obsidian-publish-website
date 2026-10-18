package mdscan

import "strings"

// StripComments removes %%...%% comments (inline and multi-line) from prose.
// Ambiguities around code spans resolve toward hiding text.
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
			if seg.Code {
				// Fail closed: an unbalanced %% inside a code span opens a
				// comment; %% inside code never closes one.
				if !in {
					if strings.Count(seg.Text, "%%")%2 == 1 {
						before, _, _ := strings.Cut(seg.Text, "%%")
						b.WriteString(before)
						in, touched = true, true
					} else { // balanced pairs inside code are literal
						b.WriteString(seg.Text)
					}
				}
				continue
			}
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
