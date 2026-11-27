#!/usr/bin/env bash
# Renders plugin/preview.png, the marketplace preview, offscreen: the bar
# icon, the quick panel and the window drawn by quickshell against the
# installed omarchy-shell (its current theme) and tests/FakeService.qml
# filled with made-up sample data (tests/preview/shell.qml). Never a
# screenshot of a real Slack: no real people or messages end up in it.
#
# Usage: plugin/tests/preview/run.sh [output.png]
# Renders at PREVIEW_SCALE (default 2) for HiDPI screens. Needs quickshell
# and ImageMagick (magick).
set -euo pipefail

plugin=$(cd "$(dirname "$0")/../.." && pwd)
shell_root=${OMARCHY_SHELL:-${OMARCHY_PATH:-/usr/share/omarchy}/shell}
out=${1:-$plugin/preview.png}
timeout_s=${PREVIEW_TIMEOUT:-30}
scale=${PREVIEW_SCALE:-2}

dir=$(mktemp -d)
trap 'kill "${pid:-}" 2>/dev/null || true; rm -rf "$dir"' EXIT

for entry in "$shell_root"/*; do
  [ "$(basename "$entry")" = shell.qml ] || ln -s "$entry" "$dir/$(basename "$entry")"
done
ln -s "$plugin" "$dir/omaslack"
cp "$plugin/tests/preview/shell.qml" "$dir/shell.qml"
mkdir "$dir/shots"

log="$dir/preview.log"
OMASLACK_PREVIEW_DIR="$dir/shots" QT_QPA_PLATFORM=offscreen QT_SCALE_FACTOR="$scale" quickshell -p "$dir/shell.qml" >"$log" 2>&1 &
pid=$!
for _ in $(seq $((timeout_s * 10))); do
  grep -q "PREVIEW DONE" "$log" && break
  kill -0 "$pid" 2>/dev/null || break
  sleep 0.1
done
if ! grep -q "PREVIEW DONE" "$log" || grep -q "PREVIEW FAIL" "$log"; then
  echo "preview: rendering failed; quickshell log:" >&2
  cat "$log" >&2
  exit 1
fi

# The desktop: a bar strip across the top with the icon at its right, the
# window below it and the quick panel dropping from the icon beside it.
s="$dir/shots"
colors=$(grep -o 'PREVIEW COLORS.*' "$log" | head -n 1)
bg=$(sed -n 's/.*bg=\(#[0-9a-fA-F]*\).*/\1/p' <<<"$colors")
bar=$(sed -n 's/.*bar=\(#[0-9a-fA-F]*\).*/\1/p' <<<"$colors")
read -r ww wh < <(magick identify -format '%w %h\n' "$s/window.png")
read -r iw ih < <(magick identify -format '%w %h\n' "$s/icon.png")
read -r pw ph < <(magick identify -format '%w %h\n' "$s/panel.png")
margin=$((20 * scale))
cw=$((ww + pw + margin * 3))
ch=$((ih + margin + (wh > ph ? wh : ph) + margin))
ix=$((cw - margin - pw / 2 - iw / 2))
magick -size "${cw}x${ch}" "xc:$bg" \
  -fill "$bar" -draw "rectangle 0,0 $((cw - 1)),$((ih - 1))" \
  "$s/icon.png" -geometry "+$ix+0" -composite \
  "$s/window.png" -geometry "+$margin+$((ih + margin))" -composite \
  "$s/panel.png" -geometry "+$((cw - margin - pw))+$((ih + 3 * scale))" -composite \
  -strip "$out"
echo "preview: wrote $out ($(magick identify -format '%wx%h' "$out"))"
