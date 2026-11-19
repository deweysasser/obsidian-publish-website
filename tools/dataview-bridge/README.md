# dataview-bridge

Bundles [Dataview](https://github.com/blacksmithgu/obsidian-dataview)'s own page
parser (`parsePage`) and query engine (`executeTable/List/Task/Inline`) into
`internal/dataview/bridge.js`, which the Go program embeds and runs with Node.
Only the parts of the `obsidian` module the engine touches are stubbed
(`stubs/`). The Go side supplies Obsidian-equivalent `CachedMetadata`
(`internal/obsmeta`).

Protocol: JSON lines. On stdin, a header (`settings`, `files` (the note count),
`attachments[]`, `paths[]`, `aliases{}`, `requests[]`), then one line per note;
on stdout, one line per result. One JSON document would not do: Node cannot
hold a string over ~512 MiB, which a large vault's request exceeds. See
`entry.ts`.

## Rebuild / upgrade Dataview

```bash
cd tools/dataview-bridge
npm ci
npm run build                                   # pinned tag
DATAVIEW_TAG=0.5.69 DATAVIEW_SHA256=<sum> npm run build   # upgrade
```

Then run `go test ./...` and the oracle (`tools/dv-oracle`).
