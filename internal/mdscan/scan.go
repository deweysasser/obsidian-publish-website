// Package mdscan provides a code-aware scanner for Obsidian markdown so
// rewriters and extractors never touch fenced code, inline code or math.
package mdscan

import (
	"regexp"
	"strings"
)

// Line is one source line with fence state.
type Line struct {
	Text       string // full line text, without trailing newline
	Num        int    // 1-based line number in the original file
	Quote      string // blockquote prefix (e.g. "> > ") stripped for fence detection
	InFence    bool   // strictly inside a fence (not the open/close lines)
	FenceOpen  bool
	FenceClose bool
	Lang       string // info-string language on the opening line
}

var (
	quoteRe = regexp.MustCompile(`^(\s{0,3}>\s?)+`)
	fenceRe = regexp.MustCompile("^([ \\t]*)(`{3,}|~{3,})\\s*([^\\s`]*)") // any indent: fences nest in list items
)

// Lines splits body into lines and tracks fenced code blocks, including
// fences nested in blockquotes/callouts.
func Lines(body []byte, firstLine int) []Line {
	text := strings.TrimSuffix(string(body), "\n")
	if text == "" && len(body) == 0 {
		return nil
	}
	raw := strings.Split(text, "\n")
	out := make([]Line, len(raw))
	var fence string // active fence marker, "" when not in a fence
	for i, t := range raw {
		t = strings.TrimSuffix(t, "\r")
		l := Line{Text: t, Num: firstLine + i}
		l.Quote = quoteRe.FindString(t)
		inner := t[len(l.Quote):]
		m := fenceRe.FindStringSubmatch(inner)
		switch {
		case fence == "" && m != nil:
			fence = m[2]
			l.FenceOpen = true
			l.Lang = strings.ToLower(m[3])
		case fence != "" && m != nil && m[2][0] == fence[0] && len(m[2]) >= len(fence) && strings.TrimSpace(inner[len(m[0]):]) == "" && m[3] == "":
			fence = ""
			l.FenceClose = true
		case fence != "":
			l.InFence = true
		}
		out[i] = l
	}
	return out
}

// Segment is a run of prose or code within a single line.
type Segment struct {
	Text string
	Code bool // inline code span or math; never rewrite
	Math bool
}

// Inline splits a prose line into segments, isolating `code` spans and
// $math$ / $$math$$ spans.
func Inline(s string) []Segment {
	var out []Segment
	prose := 0
	flush := func(end int) {
		if end > prose {
			out = append(out, Segment{Text: s[prose:end]})
		}
	}
	for i := 0; i < len(s); {
		switch s[i] {
		case '\\':
			i += 2
			continue
		case '`':
			n := run(s, i, '`')
			if end := findBackticks(s, i+n, n); end >= 0 {
				flush(i)
				out = append(out, Segment{Text: s[i : end+n], Code: true})
				i = end + n
				prose = i
				continue
			}
			i += n
			continue
		case '$':
			if end, ok := findMath(s, i); ok {
				flush(i)
				out = append(out, Segment{Text: s[i:end], Code: true, Math: true})
				i = end
				prose = i
				continue
			}
		}
		i++
	}
	flush(len(s))
	return out
}

func run(s string, i int, c byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == c {
		n++
	}
	return n
}

// findBackticks finds a closing run of exactly n backticks at or after from.
func findBackticks(s string, from, n int) int {
	for i := from; i < len(s); {
		if s[i] == '`' {
			m := run(s, i, '`')
			if m == n {
				return i
			}
			i += m
			continue
		}
		i++
	}
	return -1
}

// findMath returns the end of a math span starting at s[i]=='$'.
// Obsidian rules: $$...$$ anywhere; $x$ requires no space after the opening
// and before the closing $, and the closing $ must not be followed by a digit.
func findMath(s string, i int) (int, bool) {
	if strings.HasPrefix(s[i:], "$$") {
		if j := strings.Index(s[i+2:], "$$"); j > 0 {
			return i + 2 + j + 2, true
		}
		return 0, false
	}
	if i+1 >= len(s) || s[i+1] == ' ' || s[i+1] == '$' {
		return 0, false
	}
	for j := i + 1; j < len(s); j++ {
		if s[j] == '\\' {
			j++
			continue
		}
		if s[j] == '$' {
			if s[j-1] == ' ' {
				return 0, false
			}
			if j+1 < len(s) && s[j+1] >= '0' && s[j+1] <= '9' {
				return 0, false
			}
			return j + 1, true
		}
	}
	return 0, false
}
