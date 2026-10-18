package mdscan

import (
	"reflect"
	"testing"
)

func TestLinesFences(t *testing.T) {
	ls := Lines([]byte("a\n```dataview\nTABLE x\n```\n~~~\n[[y]]\n~~~\nb\n> ```dataview\n> LIST\n> ```\n````\n```\n````\n"), 1)
	type exp struct {
		open, in, close bool
		lang            string
	}
	want := []exp{
		{}, {open: true, lang: "dataview"}, {in: true}, {close: true},
		{open: true}, {in: true}, {close: true}, {},
		{open: true, lang: "dataview"}, {in: true}, {close: true},
		{open: true}, {in: true}, {close: true},
	}
	if len(ls) != len(want) {
		t.Fatalf("got %d lines", len(ls))
	}
	for i, w := range want {
		l := ls[i]
		if l.FenceOpen != w.open || l.InFence != w.in || l.FenceClose != w.close || l.Lang != w.lang || l.Num != i+1 {
			t.Errorf("line %d %q: %+v want %+v", i+1, l.Text, l, w)
		}
	}
	if ls[9].Quote != "> " {
		t.Errorf("quote prefix %q", ls[9].Quote)
	}
}

func TestLinesUnclosedFenceRunsToEnd(t *testing.T) {
	ls := Lines([]byte("```\n[[x]]\n"), 5)
	if !ls[1].InFence || ls[1].Num != 6 {
		t.Errorf("%+v", ls[1])
	}
}

func TestInlineCodeAndMath(t *testing.T) {
	got := Inline("a `[[x]]` b $y^2$ c ``d`e`` f $5 and $6")
	want := []Segment{
		{Text: "a "}, {Text: "`[[x]]`", Code: true}, {Text: " b "},
		{Text: "$y^2$", Code: true, Math: true}, {Text: " c "}, {Text: "``d`e``", Code: true},
		{Text: " f $5 and $6"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v", got)
	}
}

func TestInlineUnmatchedBacktick(t *testing.T) {
	got := Inline("a `b")
	if len(got) != 1 || got[0].Code {
		t.Errorf("got %#v", got)
	}
}

func TestInlineDisplayMath(t *testing.T) {
	got := Inline("x $$a+b$$ y")
	if len(got) != 3 || !got[1].Math || got[1].Text != "$$a+b$$" {
		t.Errorf("got %#v", got)
	}
}
