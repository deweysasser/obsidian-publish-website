# obsidian-publish-website

## Purpose

This project is a **generally useful tool for publishing an Obsidian vault**
as a website. It is not the codebase of any one site.

Features that only one site uses today are fine. But **question any feature
that would make the tool usable only by a single site**, before building it
and when reviewing it. Typical signs:

- One site's domain words in code, templates, assets or defaults (for
  example "town" or "state" for a local-government site).
- Behaviour hardwired to one site's infrastructure, accounts or URLs.

The usual answer is to make the site-specific part configuration. The
generator ships a neutral default, and each site declares its own kinds,
labels, fields and endpoints in its `publish.yaml`.

## Browser code

- **TypeScript:** non-trivial browser code is TypeScript, in `web/src/`. It
  compiles (`make web`) to `internal/target/html/assets/*.js`, which is
  committed. Never hand-edit the compiled `.js`.
- **Containers only:** never run `npm`, `npx` or a package script on the
  host; use the Makefile's `web*` targets, which run them in the pinned Node
  container. `ignore-scripts` stays on, dependencies stay few, and new ones
  are resolved `--before` a date at least two weeks old.
- **Tests:** `web/test/*.test.ts`, run by Node's built-in test runner
  against the compiled assets in happy-dom (`make web-test`).
