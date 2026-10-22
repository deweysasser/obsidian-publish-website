#!/bin/sh
# Regenerates internal/render/usmap.json from Census Bureau cartographic
# boundary files (public domain). mapshaper runs in a pinned container; no
# npm code runs on the host.
set -eu

URL=https://www2.census.gov/geo/tiger/GENZ2023/shp/cb_2023_us_state_500k.zip
SHA256=4a9b4f5cf993cd23738ac49b58fbb556f1f097fcf5e404a9dc10348dd41f7432
NODE_IMAGE=node:22.11.0-bookworm-slim
MAPSHAPER=mapshaper@0.7.71

repo=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/src" "$work/out"

curl -sSfL -o "$work/states.zip" "$URL"
echo "$SHA256  $work/states.zip" | sha256sum -c -
(cd "$work/src" && unzip -q ../states.zip)

# States are simplified hard; territories (small islands) much less, or
# they collapse to slivers.
docker run --rm -v "$work/src:/src:ro" -v "$work/out:/out" "$NODE_IMAGE" sh -c "
  npm install -g --no-fund --no-audit --no-update-notifier $MAPSHAPER >/dev/null 2>&1 &&
  mapshaper -i /src/cb_2023_us_state_500k.shp -filter-fields STUSPS,NAME \
    -filter-islands min-area=5km2 -simplify 3% keep-shapes -clean \
    -filter '[\"PR\",\"VI\",\"GU\",\"MP\",\"AS\"].indexOf(STUSPS) < 0' \
    -o /out/states.json format=geojson precision=0.0001 &&
  mapshaper -i /src/cb_2023_us_state_500k.shp -filter-fields STUSPS,NAME \
    -filter '[\"PR\",\"VI\",\"GU\",\"MP\",\"AS\"].indexOf(STUSPS) >= 0' \
    -filter-islands min-area=5km2 -simplify 20% keep-shapes -clean \
    -o /out/territories.json format=geojson precision=0.0001 &&
  chown $(id -u):$(id -g) /out/*"

cd "$repo"
go run ./tools/usmap -in "$work/out/states.json,$work/out/territories.json" -out internal/render/usmap.json
