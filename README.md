# obsidian-publish-website

Publish a folder of an Obsidian vault as a website, with Dataview queries
evaluated by Dataview itself.

- **Targets:** a complete static site (`html`, default — GitHub Pages ready),
  or content exports for a site you control (`hugo`, `jekyll`).
- **Dataview:** `TABLE`, `LIST`, `TASK` and inline `` `= expr` `` queries are
  run by the real Dataview engine (bundled, pinned release) against the whole
  vault, so results match Obsidian.
- **Exclusion:** glob patterns and `publish: false` frontmatter. Links to
  anything not published render as plain text; excluded content never
  reaches the output.
- **Site features (html):** folder navigation, per-page table of contents,
  backlinks, breadcrumbs, tag pages, all-pages index, `sitemap.xml`,
  Pagefind full-text search, KaTeX math and Mermaid diagrams (vendored, no
  CDN), light/dark themes.

## Install

```bash
go install github.com/deweysasser/obsidian-publish-website@latest
```

Runtime requirements:

| Needed for | Requirement |
|---|---|
| Dataview queries | Node.js ≥ 18 on `PATH` (or `dataview: none`) |
| Search (html target) | [Pagefind](https://pagefind.app) on `PATH` — `npm i -g pagefind` (or `search: none`) |

## Usage

```bash
obsidian-publish-website build -c publish.yaml [--target html|hugo|jekyll] [-o DIR] [--strict]
```

`--strict` turns warnings (broken links, query errors, unsupported
`dataviewjs`) into a failed build. Output directories are only replaced if
they are empty or were created by this tool (they contain a
`.obsidian-publish` marker).

### publish.yaml

```yaml
vault: ~/Documents/MyVault   # vault root (relative paths are relative to this file)
root: Town Notes           # folder inside the vault to publish
exclude:                               # globs relative to root; a matching folder excludes everything in it
  - People/**
target: html                           # html | hugo | jekyll
output: ./site
search: pagefind                       # pagefind | none (html target only)
dataview: node                         # node | none (none renders queries as placeholders)
site:
  title: Town Notes
  base_url: https://example.github.io/town/   # absolute URL; its path prefixes every link
  home: Town Notes.md      # optional, relative to root; default: root folder note
html:
  template_dir: ./templates            # optional: override layout.html
hugo:
  section: town                    # content/<section>/  (default: slug of root)
jekyll:
  collection: town                 # _<collection>/      (default: slug of root)
export_frontmatter: false              # hugo/jekyll: copy note frontmatter under "obsidian:"
transforms: []                         # post-render hooks (e.g. future redaction)
```

A note is excluded if its path (relative to `root`) or any parent folder
matches an `exclude` glob, or if its frontmatter has a `publish` key that is
not `true`/`yes`. Folder notes (`Folder/Folder.md` or `Folder/index.md`)
become the folder's page.

## Targets

### html (default)

A self-contained site. Push the output directory to GitHub Pages (it includes
`.nojekyll`) or any static host. Set `site.base_url` to the final URL so
links, the sitemap and search work under a sub-path.

### hugo

Writes `content/<section>/` (leaf/branch bundles with explicit `url`) and
`static/<section>/assets/` into `output`, which can be an existing Hugo site;
only those two directories are replaced. Callouts and Dataview results are
HTML, so enable raw HTML in the Hugo config:

```toml
[markup.goldmark.renderer]
unsafe = true
```

### jekyll

Writes a collection `_<collection>/` with permalinks and
`assets/<collection>/` into `output` (e.g. a GitHub Pages Jekyll site), plus
`_config.obsidian-publish.yml` declaring the collection. Merge that into
`_config.yml`, or build with
`jekyll build --config _config.yml,_config.obsidian-publish.yml`.
Pages use `layout: page`. Note text is wrapped in `{% raw %}` so Liquid syntax
in notes is left alone.

## Obsidian syntax

Wikilinks (aliases, headings, blocks), markdown links to notes, note/heading/
block embeds (recursive, cycle-safe), image/PDF/audio/video embeds with
sizes, callouts (all types and aliases, foldable, nested), `==highlights==`,
`%%comments%%` (removed, including from metadata), tags (nested), inline
fields (`key:: value`, `[key:: value]`, `(key:: value)`), task lists,
footnotes, tables, math, Mermaid, and the `folder-index-content` block.

Line breaks follow the vault's "Strict line breaks" setting
(`.obsidian/app.json`): by default, as in Obsidian, a single newline is a
line break. For hugo/jekyll set the equivalent in that generator (Hugo
`markup.goldmark.renderer.hardWraps = true`, kramdown `hard_wrap: true`).

### Transcripts

A fenced `transcript` block renders as a collapsible timeline:

````markdown
```transcript
0:07 | Hi there.
1:02:03 | Later remark.
```
````

Lines may start with `m:ss`, `h:mm:ss` or `[m:ss]`, optionally followed by
`|` or `-`. Text is literal (no links or formatting), as Obsidian shows it.
If the note's `video` or `url` frontmatter is a YouTube URL, timestamps link
to that moment. To make the block readable in Obsidian too, add a CSS
snippet:

```css
.block-language-transcript, pre.language-transcript, pre.language-transcript code {
  white-space: pre-wrap; font-family: var(--font-text);
}
```

## Dataview

How it works: `tools/dataview-bridge` bundles Dataview's own page parser and
query engine (currently Dataview 0.5.68, MIT) from its hash-verified source
release into `internal/dataview/bridge.js`, which is embedded in the binary.
At build time the program emulates the parts of Obsidian's metadata cache
Dataview needs (`internal/obsmeta`), sends every note plus every query found
in published pages to one Node process, and renders the typed results.

Not supported (render a placeholder and a warning): `dataviewjs`, inline
`$=`, `CALENDAR` queries.

Upgrade Dataview: see `tools/dataview-bridge/README.md`.

## Publishing to Cloudflare Pages

`make publish` builds the site and deploys it with wrangler. Pagefind and
wrangler run from a locally built, pinned image (`tools/publish/Dockerfile`),
so no npm code runs on the host. The API token is read with `op read` and
streamed to the container on stdin; it is never written to disk or passed
as a container environment variable. Put settings in `publish.local.mk`
(git-ignored):

```make
PUBLISH_CONFIG = my.publish.yaml
PAGES_PROJECT = my-project
CLOUDFLARE_API_TOKEN_REF = op://Vault/Item/token-field
CLOUDFLARE_ACCOUNT_ID_REF = op://Vault/Item/account-id-field   # or CLOUDFLARE_ACCOUNT_ID = ...
```

The token needs only *Account → Cloudflare Pages → Edit*. Create the project
once with `tools/publish/wrangler pages project create NAME --production-branch main`.

## Development

```bash
make            # build
make test       # go test ./...
OBSIDIAN_PUBLISH_SMOKE_VAULT=~/vault go test ./internal/build -run Smoke -v
```

- `testdata/fixture-vault` is the end-to-end fixture.
- `tools/dv-oracle` captures ground truth from a running Obsidian
  (metadata cache and query results) for the fixture; the oracle tests skip
  until those goldens exist.
- `tools/vendor.sh` refreshes vendored KaTeX/Mermaid.

Project scaffolding comes from
[golang-program](https://github.com/deweysasser/golang-program); see
`README-TEMPLATE.md`.
