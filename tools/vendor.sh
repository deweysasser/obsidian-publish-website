#!/usr/bin/env bash
# Vendors KaTeX and Mermaid into the html target's embedded assets so
# published sites need no CDN. Re-run to upgrade (edit versions below).
set -euo pipefail
KATEX=0.16.11
MERMAID=11.4.1
DEST="$(cd "$(dirname "$0")/.." && pwd)/internal/target/html/assets/vendor"
CDN=https://cdn.jsdelivr.net/npm
rm -rf "$DEST" && mkdir -p "$DEST/katex/fonts" "$DEST/mermaid"
curl -fsSL "$CDN/katex@$KATEX/dist/katex.min.js" -o "$DEST/katex/katex.min.js"
curl -fsSL "$CDN/katex@$KATEX/dist/katex.min.css" -o "$DEST/katex/katex.min.css"
curl -fsSL "$CDN/katex@$KATEX/dist/contrib/auto-render.min.js" -o "$DEST/katex/auto-render.min.js"
for f in $(grep -o 'fonts/[A-Za-z0-9_-]*\.woff2' "$DEST/katex/katex.min.css" | sort -u); do
  curl -fsSL "$CDN/katex@$KATEX/dist/$f" -o "$DEST/katex/$f"
done
curl -fsSL "$CDN/mermaid@$MERMAID/dist/mermaid.min.js" -o "$DEST/mermaid/mermaid.min.js"
echo "KaTeX $KATEX, Mermaid $MERMAID" > "$DEST/VERSIONS"
