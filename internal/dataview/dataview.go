// Package dataview evaluates Dataview queries with the real Dataview engine,
// bundled in bridge.js (see tools/dataview-bridge) and run under Node.
package dataview

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/deweysasser/obsidian-publish-website/internal/index"
	"github.com/deweysasser/obsidian-publish-website/internal/mdscan"
	"github.com/deweysasser/obsidian-publish-website/internal/obsmeta"
)

//go:embed bridge.js
var bridgeJS []byte

// Request is one query or inline expression to evaluate.
type Request struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"` // "query" or "inline"
	Origin string `json:"origin"`
	Source string `json:"source"`
}

// Task is a task/list item in a TASK result.
type Task struct {
	Text      string `json:"text"`
	Status    string `json:"status"`
	Path      string `json:"path"`
	Task      bool   `json:"task"`
	Checked   bool   `json:"checked"`
	Completed bool   `json:"completed"`
	Line      int    `json:"line"`
	Children  []Task `json:"children"`
}

// TaskGroup is a group of tasks; Key is nil for ungrouped results.
type TaskGroup struct {
	Key   *Value
	Tasks []Task
}

// Result is the outcome of one Request.
type Result struct {
	ID          string          `json:"id"`
	OK          bool            `json:"ok"`
	Error       string          `json:"error"`
	Unsupported bool            `json:"unsupported"`
	Type        string          `json:"type"`
	Headers     []string        `json:"headers"`
	IDColumn    bool            `json:"idColumn"`
	Grouped     bool            `json:"grouped"`
	Rows        [][]Value       `json:"rows"`
	Items       []Value         `json:"items"`
	Value       Value           `json:"value"`
	RawTasks    json.RawMessage `json:"tasks"`
	TaskGroups  []TaskGroup     `json:"-"`
}

// File is one vault note as sent to the bridge.
type File struct {
	Path     string                  `json:"path"`
	Content  string                  `json:"content"`
	Ctime    int64                   `json:"ctime"`
	Mtime    int64                   `json:"mtime"`
	Size     int64                   `json:"size"`
	Metadata *obsmeta.CachedMetadata `json:"metadata"`
}

// Input is the full bridge request.
type Input struct {
	Settings    map[string]any    `json:"settings"`
	Files       []File            `json:"files"`
	Attachments []string          `json:"attachments"`
	Paths       []string          `json:"paths"` // other notes: resolvable link targets with no data
	Aliases     map[string]string `json:"aliases"`
	Requests    []Request         `json:"requests"`
}

// Engine runs the bridge.
type Engine struct {
	Node     string
	Settings map[string]any
	ix       *index.Index
	include  func(string) bool
}

var nodeVersionRe = regexp.MustCompile(`^v(\d+)\.`)

// FindNode locates a Node.js >= 18 binary on PATH.
func FindNode() (string, error) {
	p, err := exec.LookPath("node")
	if err != nil {
		return "", errors.New(`dataview queries need Node.js >= 18 on PATH (https://nodejs.org); install it or set "dataview: none" to render queries as placeholders`)
	}
	out, err := exec.Command(p, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("running %s --version: %w", p, err)
	}
	m := nodeVersionRe.FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return "", fmt.Errorf("unrecognized node version %q", out)
	}
	if v, _ := strconv.Atoi(m[1]); v < 18 {
		return "", fmt.Errorf(`node %s is too old; dataview queries need Node.js >= 18 (or set "dataview: none")`, strings.TrimSpace(string(out)))
	}
	return p, nil
}

// LoadSettings reads the vault's Dataview plugin settings (empty if absent).
func LoadSettings(vaultRoot string) (map[string]any, error) {
	b, err := os.ReadFile(filepath.Join(vaultRoot, ".obsidian", "plugins", "dataview", "data.json"))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	s := map[string]any{}
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("dataview settings: %w", err)
	}
	return s, nil
}

// FilesFromIndex converts the notes accepted by include (nil = all) into
// bridge input. Content has CRLF normalized and %% comments blanked so
// Dataview never parses fields from comments.
func FilesFromIndex(ix *index.Index, include func(string) bool) []File {
	var out []File
	for _, p := range ix.Paths {
		if include != nil && !include(p) {
			continue
		}
		n := ix.Pages[p].Note
		out = append(out, File{Path: p, Content: mdscan.BlankComments(n.Raw), Ctime: n.Ctime.UnixMilli(), Mtime: n.Mtime.UnixMilli(),
			Size: n.Size, Metadata: obsmeta.Build(n)})
	}
	return out
}

// NewEngine creates an engine over the notes accepted by include (nil = the
// whole vault). Other notes stay resolvable as link targets but expose no data.
func NewEngine(node string, settings map[string]any, ix *index.Index, include func(string) bool) *Engine {
	return &Engine{Node: node, Settings: settings, ix: ix, include: include}
}

// RunIndex evaluates requests against the engine's index.
func (e *Engine) RunIndex(reqs []Request) (map[string]Result, error) {
	in := Input{Settings: e.Settings, Files: FilesFromIndex(e.ix, e.include), Aliases: map[string]string{}, Requests: reqs}
	in.Attachments = e.ix.Vault.Attachments
	for _, p := range e.ix.Paths {
		if e.include != nil && !e.include(p) {
			in.Paths = append(in.Paths, p)
			continue // aliases of hidden notes are not exposed
		}
		for _, a := range e.ix.Pages[p].Aliases {
			k := strings.ToLower(a)
			if _, dup := in.Aliases[k]; !dup {
				in.Aliases[k] = p
			}
		}
	}
	return e.run(in)
}

// Run evaluates requests over the given files.
func (e *Engine) Run(files []File, reqs []Request) (map[string]Result, error) {
	return e.run(Input{Settings: e.Settings, Files: files, Aliases: map[string]string{}, Requests: reqs})
}

func (e *Engine) run(in Input) (map[string]Result, error) {
	if in.Attachments == nil {
		in.Attachments = []string{}
	}
	if in.Paths == nil {
		in.Paths = []string{}
	}
	if in.Settings == nil {
		in.Settings = map[string]any{}
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	// .cjs: a stray package.json with "type":"module" in TMPDIR must not
	// change how Node loads the CommonJS bundle.
	f, err := os.CreateTemp("", "obsidian-publish-dataview-*.cjs")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(bridgeJS); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()
	cmd := exec.Command(e.Node, f.Name())
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("dataview bridge failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var out struct {
		Results []Result `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("dataview bridge output: %w", err)
	}
	res := map[string]Result{}
	for _, r := range out.Results {
		if r.Type == "task" {
			r.TaskGroups = decodeTasks(r.RawTasks)
		}
		res[r.ID] = r
	}
	return res, nil
}

// decodeTasks accepts either a flat task list or [{key, rows}] groupings.
func decodeTasks(raw json.RawMessage) []TaskGroup {
	var elems []json.RawMessage
	if json.Unmarshal(raw, &elems) != nil {
		return nil
	}
	var groups []TaskGroup
	var flat []Task
	for _, e := range elems {
		var probe struct {
			Key  *Value          `json:"key"`
			Rows json.RawMessage `json:"rows"`
		}
		if json.Unmarshal(e, &probe) == nil && probe.Rows != nil {
			for _, g := range decodeTasks(probe.Rows) {
				key := probe.Key
				if g.Key != nil {
					key = g.Key // nested groupings: innermost key wins
				}
				groups = append(groups, TaskGroup{Key: key, Tasks: g.Tasks})
			}
			continue
		}
		var t Task
		if json.Unmarshal(e, &t) == nil {
			flat = append(flat, t)
		}
	}
	if len(flat) > 0 {
		groups = append([]TaskGroup{{Tasks: flat}}, groups...)
	}
	return groups
}
