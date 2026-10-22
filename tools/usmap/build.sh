#!/bin/sh
# Regenerates internal/usmap/usmap.json (state and territory shapes) and
# internal/usmap/places.tsv.gz (place locations) from Census Bureau
# cartographic boundary and gazetteer files (public domain). mapshaper runs
# in a pinned container; no npm code runs on the host.
set -eu

URL=https://www2.census.gov/geo/tiger/GENZ2023/shp/cb_2023_us_state_500k.zip
SHA256=4a9b4f5cf993cd23738ac49b58fbb556f1f097fcf5e404a9dc10348dd41f7432
GAZ=https://www2.census.gov/geo/docs/maps-data/data/gazetteer/2023_Gazetteer
GAZ_SHA256="3c8b23c0dcdb15c0ebd16fee7373e032466e6b585650aa219bc1bee5ac8bd58f  2023_Gaz_place_national.zip
22f4892eadaa4236add3b3dd015dff401e74e1aef32a7717a15e44c23dbdc1f3  2023_Gaz_cousubs_national.zip
919df59ba90759cce85468c0337e898e4b39c08eaffce86ddd88fa41f1f7f0c8  2023_Gaz_counties_national.zip"
NODE_IMAGE=node:22.11.0-bookworm-slim
MAPSHAPER=mapshaper@0.7.71

repo=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/src" "$work/out" "$work/gaz"

curl -sSfL -o "$work/states.zip" "$URL"
echo "$SHA256  $work/states.zip" | sha256sum -c -
(cd "$work/src" && unzip -q ../states.zip)
for f in place cousubs counties; do
  curl -sSfL -o "$work/gaz/2023_Gaz_${f}_national.zip" "$GAZ/2023_Gaz_${f}_national.zip"
done
(cd "$work/gaz" && echo "$GAZ_SHA256" | sha256sum -c - && for z in *.zip; do unzip -q "$z"; done)

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
g="$work/gaz/2023_Gaz"
go run ./tools/usmap -in "$work/out/states.json,$work/out/territories.json" \
  -gazetteer "${g}_place_national.txt,${g}_cousubs_national.txt,${g}_counties_national.txt"
