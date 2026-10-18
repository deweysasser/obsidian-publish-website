# dataview-bridge

Bundles [Dataview](https://github.com/blacksmithgu/obsidian-dataview)'s own page
parser (`parsePage`) and query engine (`executeTable/List/Task/Inline`) into
`internal/dataview/bridge.js`, which the Go program embeds and runs with Node.
Only the parts of the `obsidian` module the engine touches are stubbed
(`stubs/`). The Go side supplies Obsidian-equivalent `CachedMetadata`
(`internal/obsmeta`).

Protocol: one JSON request on stdin (`settings`, `files[]`, `attachments[]`,
`aliases{}`, `requests[]`), one JSON response on stdout (`results[]`); see
`entry.ts`.

## Rebuild / upgrade Dataview

```bash
cd tools/dataview-bridge
npm ci
npm run build                                   # pinned tag
DATAVIEW_TAG=0.5.69 DATAVIEW_SHA256=<sum> npm run build   # upgrade
```

Then run `go test ./...` and the oracle (`tools/dv-oracle`).
