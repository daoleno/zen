#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ASSETS="$ROOT/docs/assets"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

for state in chat sessions brain stats; do
  test -s "$ASSETS/zen-$state.png" || {
    echo "missing raw screenshot: $ASSETS/zen-$state.png" >&2
    exit 1
  }
done

test -s "$ASSETS/zen-readme-background.webp" || {
  echo "missing hero background: $ASSETS/zen-readme-background.webp" >&2
  exit 1
}

make_phone() {
  local input="$1"
  local output="$2"
  local width="$3"
  local height="$4"
  local radius="$5"
  local bezel="$6"
  local inner="$TMP/$(basename "$output").inner.png"
  local mask="$TMP/$(basename "$output").mask.png"

  magick "$input" -resize "${width}x${height}!" "$inner"
  magick -size "${width}x${height}" xc:none \
    -fill white -draw "roundrectangle 0,0,$((width - 1)),$((height - 1)),$radius,$radius" \
    "$mask"
  magick "$inner" "$mask" -alpha off -compose CopyOpacity -composite "$inner"
  magick -size "$((width + bezel * 2))x$((height + bezel * 2))" xc:none \
    -fill '#202822' \
    -draw "roundrectangle 0,0,$((width + bezel * 2 - 1)),$((height + bezel * 2 - 1)),$((radius + bezel)),$((radius + bezel))" \
    "$inner" -geometry "+$bezel+$bezel" -compose over -composite "$output"
}

make_feature() {
  local state="$1"
  local accent="$2"
  local phone="$TMP/$state-feature-phone.png"
  local shadow="$TMP/$state-feature-shadow.png"
  local base="$TMP/$state-feature-base.png"

  make_phone "$ASSETS/zen-$state.png" "$phone" 480 1039 34 9
  magick "$phone" \( +clone -background '#26332B66' -shadow 72x22+0+22 \) \
    +swap -background none -layers merge "$shadow"
  magick -size 700x1200 xc:'#F4F5F1' \
    -fill "$accent" -draw 'circle 620,90 780,90' \
    -fill '#E6EBE5' -draw 'circle 40,1110 210,1110' \
    -stroke '#CBD6CD' -strokewidth 3 -fill none -draw 'circle 650,1060 770,1060' \
    "$base"
  magick "$base" "$shadow" -gravity center -geometry +0+0 -compose over -composite \
    -quality 88 "$ASSETS/zen-$state.webp"
}

make_feature chat '#DCE7DE'
make_feature sessions '#E3EEE5'
make_feature brain '#D7E4DA'
make_feature stats '#E7EBE4'

hero="$TMP/hero-base.png"
magick "$ASSETS/zen-readme-background.webp" \
  -resize '1800x1000^' -gravity center -extent 1800x1000 \
  -modulate 102,82,100 \
  -fill '#F4F2EA18' -colorize 9 \
  "$hero"

states=(chat sessions brain stats)
xs=(120 540 960 1380)
ys=(125 205 125 205)
angles=(-2 2 -2 2)

for index in "${!states[@]}"; do
  state="${states[$index]}"
  phone="$TMP/$state-hero-phone.png"
  shadow="$TMP/$state-hero-shadow.png"
  make_phone "$ASSETS/zen-$state.png" "$phone" 320 693 24 7
  magick "$phone" \( +clone -background '#26332B70' -shadow 74x20+0+18 \) \
    +swap -background none -layers merge -rotate "${angles[$index]}" "$shadow"
  magick "$hero" "$shadow" -geometry "+${xs[$index]}+${ys[$index]}" \
    -compose over -composite "$hero"
done

magick "$hero" -quality 90 "$ASSETS/zen-overview.webp"

echo "Rendered README screenshot assets in $ASSETS"
