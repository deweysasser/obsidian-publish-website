# dv-oracle

Ground truth for the two places correctness depends on emulating Obsidian:

- `internal/obsmeta` must produce the same `CachedMetadata` Obsidian does
  (sections, list items/parents, links, tags, headings, positions).
- The bridge must give the same query results as Dataview inside Obsidian.

## Regenerate

1. Open `testdata/fixture-vault` as a vault in Obsidian (≥ 1.12, CLI enabled).
2. Install and enable Dataview 0.5.68 (copy `.obsidian/plugins/dataview/` from
   any vault; only `data.json` is committed).
3. `node tools/dv-oracle/oracle.mjs fixture-vault`
4. `go test ./internal/obsmeta/ ./internal/dataview/ -run Oracle -v`

The `obsidian eval` syntax can differ between Obsidian versions; check
`obsidian help eval` if the script fails. Tests skip when the goldens are absent.
