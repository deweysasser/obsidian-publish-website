package dataview

import (
	"encoding/json"
	"fmt"
)

// Kind is the type of a Dataview value as returned by the bridge.
type Kind int

const (
	KNull Kind = iota
	KBool
	KNumber
	KString
	KDate     // Str holds Dataview's formatted text
	KDuration // Str holds Dataview's formatted text
	KLink
	KList
	KObject
	KPair         // list item "id: value" pair (LIST queries)
	KExternalLink // URL + Str (display)
	KText         // pre-rendered markdown (other widgets, functions)
)

// Link is a Dataview link value; Path is a resolved vault path when the
// target exists, otherwise the raw link text.
type Link struct {
	Path, Subpath, Type, Display string
	Embed                        bool
}

// Value is a decoded Dataview literal.
type Value struct {
	Kind Kind
	Bool bool
	Num  float64
	Str  string
	URL  string
	ISO  string // dates: ISO timestamp
	Link Link
	List []Value
	Keys []string
	Obj  map[string]Value
	Pair [2]*Value
}

func (v *Value) UnmarshalJSON(b []byte) error {
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*v = decode(raw)
	return nil
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func decode(raw any) Value {
	switch x := raw.(type) {
	case nil:
		return Value{Kind: KNull}
	case bool:
		return Value{Kind: KBool, Bool: x}
	case float64:
		return Value{Kind: KNumber, Num: x}
	case string:
		return Value{Kind: KString, Str: x}
	case []any:
		v := Value{Kind: KList}
		for _, e := range x {
			v.List = append(v.List, decode(e))
		}
		return v
	case map[string]any:
		switch {
		case x["$date"] != nil:
			return Value{Kind: KDate, Str: str(x, "text"), ISO: str(x, "$date")}
		case x["$dur"] != nil:
			ms, _ := x["$dur"].(float64)
			return Value{Kind: KDuration, Str: str(x, "text"), Num: ms}
		case x["$link"] != nil:
			emb, _ := x["embed"].(bool)
			return Value{Kind: KLink, Link: Link{Path: str(x, "$link"), Subpath: str(x, "subpath"), Type: str(x, "type"), Display: str(x, "display"), Embed: emb}}
		case x["$obj"] != nil:
			v := Value{Kind: KObject, Obj: map[string]Value{}}
			entries, _ := x["$obj"].([]any)
			for _, e := range entries {
				kv, _ := e.([]any)
				if len(kv) != 2 {
					continue
				}
				k := fmt.Sprint(kv[0])
				v.Keys = append(v.Keys, k)
				v.Obj[k] = decode(kv[1])
			}
			return v
		case x["$pair"] != nil:
			kv, _ := x["$pair"].([]any)
			if len(kv) != 2 {
				return Value{Kind: KNull}
			}
			a, b := decode(kv[0]), decode(kv[1])
			return Value{Kind: KPair, Pair: [2]*Value{&a, &b}}
		case x["$elink"] != nil:
			return Value{Kind: KExternalLink, URL: str(x, "$elink"), Str: str(x, "display")}
		case x["$widget"] != nil:
			return Value{Kind: KText, Str: str(x, "$widget")}
		case x["$text"] != nil:
			return Value{Kind: KText, Str: str(x, "$text")}
		case x["$function"] != nil:
			return Value{Kind: KText, Str: "<function>"}
		}
	}
	return Value{Kind: KText, Str: fmt.Sprint(raw)}
}
