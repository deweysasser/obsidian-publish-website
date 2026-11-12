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

## Quick start

```bash
cat > publish.yaml <<'YAML'
vault: ~/Documents/MyVault
root: Town Notes            # folder inside the vault to publish
exclude: ["**/People/**"]
site:
  title: Town Notes
  base_url: http://localhost:8000/
YAML

obsidian-publish-website build          # reads ./publish.yaml, writes ./site
python3 -m http.server -d site 8000     # preview at http://localhost:8000/
```

For a real deployment set `base_url` to the final URL (see
[html](#html-default)) and either push `site/` to any static host or use
[`publish`](#publishing) to deploy to Cloudflare Pages or R2.

## Install

```bash
go install github.com/deweysasser/obsidian-publish-website@latest   # Go ≥ 1.27
```

or download a prebuilt binary (Linux, macOS, Windows) from the
[Releases](https://github.com/deweysasser/obsidian-publish-website/releases)
page, or use the [container image](#container), which needs nothing else
installed.

Runtime requirements for the binary (the [container](#container) has them all):

| Needed for | Requirement |
|---|---|
| Dataview queries | Node.js ≥ 18 on `PATH` (or `dataview: none`) |
| Search (html target), Pages deploy | Docker (or `tools.runner: host` with Pagefind / wrangler on `PATH`; or `search: none`) |

## Container

`ghcr.io/deweysasser/obsidian-publish-website` (`:latest`, `:<version>`,
`:main`) bundles everything the program uses: Node for Dataview, Pagefind
and wrangler. Inside it, tools run directly; it never starts another
container. Mount the vault (read-only is fine) and a working directory
holding `publish.yaml`; the site is written relative to the config, so it
lands in that directory:

```bash
docker run --rm --user "$(id -u):$(id -g)" \
  -v ~/Documents/MyVault:/vault:ro -v "$PWD":/work \
  -e CLOUDFLARE_API_TOKEN -e CLOUDFLARE_ACCOUNT_ID \
  ghcr.io/deweysasser/obsidian-publish-website publish --vault /vault
```

`--vault` overrides the config's `vault:` so the same `publish.yaml` works
on the host and in the container. Use `build` instead of `publish` to only
build. The image has no 1Password CLI, so give credentials as `env:`
references; to keep them in 1Password, wrap the command:
`op run --env-file=cf.env -- docker run ...` with `cf.env` mapping
`CLOUDFLARE_API_TOKEN=op://...`.

## Usage

```bash
obsidian-publish-website build -c publish.yaml [--target html|hugo|jekyll] [-o DIR] [--vault DIR] [--strict]
```

`-c` defaults to `./publish.yaml`; `build` is the default command.

`--strict` turns warnings (broken links, query errors, unsupported
`dataviewjs`) into a failed build. Output directories are only replaced if
they are empty or were created by this tool (they contain a
`.obsidian-publish` marker).

Files and folders whose names start with `.`, and Syncthing conflict copies
(`*.sync-conflict-*`), are never read from the vault.

### publish.yaml

```yaml
vault: ~/Documents/MyVault             # vault root (relative paths are relative to this file)
root: Town Notes                       # folder inside the vault to publish
exclude:                               # globs relative to root; a matching folder excludes everything in it
  - People/**
target: html                           # html | hugo | jekyll
output: ./site
search: pagefind                       # pagefind | none (html target only)
dataview: node                         # node | none (none renders queries as placeholders)
dataview_settings:                     # optional: override the vault's Dataview settings for the site
  defaultDateFormat: "MMM d, yyyy"
site:
  title: Town Notes
  base_url: https://example.github.io/town/   # absolute URL; its path prefixes every link
  home: Town Notes.md                  # optional, relative to root; default: root folder note
  favicon: Attachments/icon.png        # optional, relative to the vault; default: a generic icon
  hidden_tags: [MOCs]                  # optional: organizing tags to leave off the site (and nested ones)
  tags: false                          # optional: leave all tags off the site (default true)
  about: About.md                      # optional, relative to root: linked by its title from the sidebar and every page's footer
  support:                             # optional: PayPal links for ```support blocks
    page: Support.md                   # optional, relative to root: linked by its title from the sidebar and every page's footer
    paypal_me: YourName                # PayPal.me name of the account to be paid
    amounts: [1, 2, 3, 5, 7, 10]       # one-time amounts (default)
    currency: USD                      # default
    monthly:                           # optional: PayPal Subscriptions plans (create one per amount)
      - {amount: 5, plan_id: P-1AB23456CD789012E}
    paypal_client_id: AbC…             # optional: live PayPal REST app client ID; adds a "towns" box to the monthly plans
  suggest:                             # optional: a suggestion form at /suggest/, and links to it on every page
    endpoint: /api/suggest             # a path on this site; see "Suggestion form"
    # kinds: […]                       # default: one kind, "Suggest a change"
html:
  template_dir: ./templates            # optional: override layout.html
hugo:
  section: town                        # content/<section>/  (default: slug of root)
jekyll:
  collection: town                     # _<collection>/      (default: slug of root)
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
links, the sitemap, `robots.txt`, canonical URLs and search work under a
sub-path.

Each page's sidebar lists the top-level entries plus the contents of the
folders on the path to that page, so page size does not grow with the
site. Pages carry a meta description (`description` or `summary`
frontmatter, else the first substantial paragraph), a canonical URL and
Open Graph tags for link previews. Wide tables turn into labelled cards on
phones. A `_headers` file lets Cloudflare Pages (or Netlify) cache the
content-hashed CSS/JS and Pagefind's index for a year.

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
to that moment; on the html site they seek an embedded player instead (see
below). To make the block readable in Obsidian too, add a CSS snippet:

```css
.block-language-transcript, pre.language-transcript, pre.language-transcript code {
  white-space: pre-wrap; font-family: var(--font-text);
}
```

### U.S. map

A fenced `usmap` block draws a map of the 50 states, DC and the five
territories (Alaska, Hawaii and the territories as insets; small Northeast
states also get labelled squares):

````markdown
```usmap
US
```
````

The argument is a published folder, relative to the note's folder or the
published root (empty: the note's own folder). Each state or territory with
a published subfolder or note there, named like `Massachusetts`,
`U.S. Virgin Islands` or `MA`, becomes a link; the rest are greyed out.

A fenced `statemap` block draws one state with a dot for each town in it,
labelled where there is room, with neighbouring states (linked when they
have pages) for context:

````markdown
```statemap
```
````

With no argument the state is the note's folder (put it in the state's
folder note); otherwise name a folder as for `usmap`. Towns are the
folder's published subfolders and notes, placed by the town page's
`coordinates: [lat, lon]` frontmatter or else by name from the Census
gazetteer (New England towns, then incorporated places, counties and
census-designated places). Towns that can't be placed are reported as
warnings.

When a state folder also holds pages that aren't places (topic pages, a
"Common Issues" folder), tag the place pages and name the tag; the maps
then plot only pages with it (or a tag nested under it) and skip the rest
without a warning. The town dots on the `usmap` follow the same rule:

```yaml
site:
  maps:
    place_tag: municipality
```

The shapes and place locations are embedded in the binary (Census Bureau
cartographic boundaries and gazetteer, public domain);
`tools/usmap/build.sh` regenerates them.

### Support page

The support page is an ordinary note in the vault. Wherever it contains a
fenced `support` block (leave the block empty), the build fills in a
button for each `site.support` amount (a PayPal.me link) and each monthly
plan (a PayPal Subscriptions link). Set `site.support.page` to the note to
link it, by its title, from every page.

With `site.support.paypal_client_id` set, the monthly plans become a small
form instead: a free-text box ("Towns you'd like us to cover") and PayPal's
subscribe button, which stores the text (up to 127 characters, folded to
ASCII) on the subscription as its `custom_id`, where it shows in the
subscription's details and in PayPal's API. Use the client ID of a **live**
REST app on the account that owns the plans; it is public by design (the
browser needs it), so never put the app's secret here. PayPal's script loads
only when someone picks an amount; without JavaScript, or if it fails to
load, the amounts work as plain subscribe links, without the towns.

### Suggestion form

With `site.suggest`, every page ends with a link per kind of suggestion
(by default just **Suggest a change**) to a form at `/suggest/`, which
carries the page's path along. The form needs a small service at
`endpoint`, on the same site; the generator itself holds no secrets:

- `GET {endpoint}` returns `{"turnstile_site_key": "…"}`, a
  [Cloudflare Turnstile](https://developers.cloudflare.com/turnstile/) site
  key. The form renders the anti-spam widget with it, so a key rotation
  needs no rebuild.
- `POST {endpoint}` receives the form as `multipart/form-data`: `kind`,
  `page`, `message`, `contact`, each kind's own fields, and
  `cf-turnstile-response`. It answers `{"ok": true}`, or
  `{"ok": false, "error": "…"}` with a message to show the visitor.

What it does with a suggestion is up to the service: file an issue, send an
email, and so on.

Declare other kinds of suggestion in the config. The list replaces the
default, so include `Suggest a change` if you still want it:

```yaml
site:
  suggest:
    endpoint: /api/suggest
    kinds:
      - id: correction                   # sent as "kind": lowercase letters, digits, -
        link: Suggest a change           # the link text on every page
        title: Suggest a change to this page   # the form heading (default: link)
        message: required                # required (default) | optional | none
      - id: event
        link: Tell us about an event
        message: optional
        message_label: Anything else?    # default "Message"
        fields:                          # extra single-line inputs
          - {name: event_name, label: Event, required: true, maxlength: 100}
          - {name: event_date, label: Date, maxlength: 40}   # maxlength default 200, at most 5000
```

Every kind also gets `page` (the path the visitor came from, read-only),
an optional `contact` ("Only if you'd like a reply. It's never
published."), and the Turnstile widget. `kind`, `page`, `message`, `contact`
and `cf-turnstile-response` can't be used as field names. The form page is
`noindex` and stays out of the sitemap and search. It needs JavaScript,
because Turnstile does. Without `site.suggest`, the output is exactly as
before.

A note can link to the form directly: `[tell us](/suggest/?kind=event)`.
Root-relative links that end in `/` or carry a query are treated as site
URLs and kept as written.

### YouTube embeds

`![caption](https://www.youtube.com/watch?v=...)` (Obsidian's video embed)
renders as a thumbnail that loads a `youtube-nocookie.com` player when
clicked; nothing else is fetched from YouTube until then. Transcript
timestamps seek that player, or open a docked one if the page has no embed.
Some channels forbid embedding: the build asks YouTube's oEmbed endpoint
about each video (answers cached for 30 days in the user cache directory,
`obsidian-publish/youtube-embeddable.json`), and refused videos keep plain
links to YouTube. Network failures count as embeddable; the page script
then falls back to a link if the player refuses.

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

## Publishing

```bash
obsidian-publish-website publish -c publish.yaml   # build, then deploy
obsidian-publish-website deploy  -c publish.yaml   # deploy the last build
```

Cloudflare Pages is supported via `wrangler pages deploy`:

```yaml
deploy:
  cloudflare_pages:
    project: my-site                          # create once: wrangler pages project create my-site
    branch: main                              # default
    account_id: env:CLOUDFLARE_ACCOUNT_ID     # or op://<vault>/<item>/<field>
    api_token: op://<vault>/<item>/<field>    # or env:CLOUDFLARE_API_TOKEN
```

`account_id` and `api_token` take a 1Password reference (`op://…`, read
with the `op` CLI), `env:NAME`, or, for `account_id` only, the literal
value. The token is never accepted inline, and is never put in a process
argument or container configuration: the docker runner hands it to the
container on stdin. It needs only *Account → Cloudflare Pages → Edit*.
`deploy` refuses to upload a directory this program did not build.

### Cloudflare R2

A site too large for Pages (it caps the file count) can be served from an R2
bucket with a custom domain instead. `deploy.r2` syncs the build to the
bucket over R2's S3 API, with no external tool:

```yaml
site:
  base_url: https://example.org/            # the bucket's public domain
deploy:
  r2:
    bucket: my-site                           # holds this site and nothing else
    account_id: env:CLOUDFLARE_ACCOUNT_ID     # default; or op://…, or the literal ID
    access_key_id: op://<vault>/<item>/username        # default env:R2_ACCESS_KEY_ID
    secret_access_key: op://<vault>/<item>/password    # default env:R2_SECRET_ACCESS_KEY
    # endpoint: https://<account_id>.r2.cloudflarestorage.com   (default)
    # concurrency: 64                         # uploads in flight
    # max_delete_fraction: 0.25               # refuse to delete more of the bucket
    purge:                                    # optional
      zone_id: <32-hex zone ID>
      api_token: op://<vault>/<item>/password # default env:CLOUDFLARE_PURGE_TOKEN
      # batch_size: 100                       # URLs per request (Free plan limit)
      # urls_per_second: 800                  # per account (Free plan limit)
      # everything_over: 5000                 # purge the whole zone above this
```

- **The bucket belongs to the site.** The deploy makes the bucket match the
  build, so anything else in it is deleted.
- **Sync.** Files whose MD5 differs from the object's ETag are uploaded,
  each in a single PUT so the ETag stays the MD5; a rebuild of an unchanged
  vault uploads nothing. Assets go up before the pages and search entry
  points that refer to them. Objects the build no longer has are deleted,
  but only after every upload succeeded. The build marker, `_headers` and
  `_redirects` are not uploaded: they are Pages control files, and R2 would
  serve them as ordinary files. Set cache headers and redirects as
  Cloudflare rules on the zone instead.
- **Mass-delete guard.** A build with no files, or one that would delete
  more than `max_delete_fraction` of the bucket's objects (beyond the first
  10), is refused before anything is uploaded: it usually means a broken
  build, such as an unmounted vault. `--allow-mass-delete` lets one run
  through; `max_delete_fraction: 1` turns the guard off.
- **Content types** come from a fixed table, not the host's MIME database,
  so every host sets the same ones. Only changed files are uploaded, so
  objects put in the bucket some other way keep their old types until
  `deploy --force-upload` re-uploads everything once.
- **Throttling.** R2 answers 429 or 503 when pushed. Those pause every
  upload for at least the `Retry-After` the server sends; other transient
  failures (timeouts, dropped connections, 5xx) are retried with backoff.
- **Purge.** Each uploaded or deleted file's URL (`base_url` + path,
  percent-encoded as the site links it) is purged, plus `dir/` for each
  `dir/index.html` and `base_url` for the root `index.html`, since pages are
  linked as `dir/`. Batches go out together, paced to `urls_per_second`.
  Above `everything_over` URLs the whole zone is purged instead, and also if
  a batch still fails transiently after its retries. A permanent failure (a
  bad token, a URL outside the zone) fails the deploy. If the sync fails
  part way, or the deploy is interrupted (SIGINT or SIGTERM), what it had
  already changed is still purged. The token needs only
  *Zone → Cache Purge → Purge* on that zone.
- **Credentials** follow the same rules as Pages: the S3 keys and the purge
  token must be `op://` or `env:` references, and are never put in a process
  argument, log line or error message. All are resolved before anything is
  uploaded. Create the S3 keys as an R2 API token with *Object Read & Write*
  on the one bucket.
- **One deploy at a time.** Two deploys to the same bucket at once can undo
  each other's uploads and deletes; a scheduler should not start one while
  the last is still running.

Both targets may be configured at once; Pages runs first. R2 logs
`Synced to R2` with `uploaded`, `deleted` and `unchanged` (and the error, if
the sync failed), then, on success, `Deployed` with `url`, `uploaded`,
`deleted`, `unchanged`, `purged` and `elapsed_ms`.

### External tools

Pagefind and wrangler run, by default, from a container image built on first
use from a Dockerfile embedded in the binary (pinned Node, wrangler and
pagefind versions), so no npm code runs on the host. Pagefind runs without
network access.

```yaml
tools:
  runner: docker   # default; or "host" to use pagefind/wrangler from PATH
  image: ""        # optional: use this pre-built image instead
```

`OBSIDIAN_PUBLISH_TOOLS_RUNNER=docker|host` overrides `tools.runner`; the
container image sets it to `host`.

## Development

```bash
make            # build
make test       # go test ./...
```

Some tests skip unless their dependencies are available:

| Test | Enable with |
|---|---|
| Dataview / hugo / jekyll output | `node` on `PATH` |
| Pagefind search | `OBSIDIAN_PUBLISH_TOOLS_RUNNER=docker` (or `host`) |
| Real Hugo / Jekyll site builds | `hugo` / `jekyll` on `PATH` |
| Smoke test against your own vault | `OBSIDIAN_PUBLISH_SMOKE_VAULT=~/vault` |

- `testdata/fixture-vault` is the end-to-end fixture.
- `tools/dv-oracle` captures ground truth from a running Obsidian
  (metadata cache and query results) for the fixture; the oracle tests skip
  until those goldens exist.
- `tools/vendor.sh` refreshes vendored KaTeX/Mermaid.

### Browser scripts

The html target's scripts are TypeScript in `web/src/`, compiled to
`internal/target/html/assets/` and embedded in the binary. The compiled
`.js` is committed, because `go install` can't run the compiler; CI fails
if it doesn't match the sources.

```bash
make web          # compile web/src/*.ts into the assets
make web-test     # type-check and run web/test (Node's test runner + happy-dom)
make web-check    # both, then fail if the committed assets are out of date
```

npm and Node run only in a container (Docker, the Node image pinned in
`internal/tools/Dockerfile`), never on the host. `npm ci` installs from the
lockfile with install scripts disabled (`web/.npmrc`); compiling and testing
run with no network. To add or update a dependency, pick a release at least
two weeks old:
`docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp -v "$PWD/web":/web -w /web <node image> npm install --save-dev --before=<date> <package>`.

Project scaffolding comes from
[golang-program](https://github.com/deweysasser/golang-program).

## License

MIT; see [LICENSE](LICENSE). The bundled Dataview, KaTeX and Mermaid are
MIT-licensed; the U.S. map data is public domain (U.S. Census Bureau).
