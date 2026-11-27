#!/usr/bin/env bash
# Runs the view smoke tests (tests/smoke/*Smoke.qml) in an offscreen
# quickshell against the installed omarchy-shell. Exits 1 on any failure.
set -euo pipefail

plugin=$(cd "$(dirname "$0")/../.." && pwd)
shell_root=${OMARCHY_SHELL:-${OMARCHY_PATH:-/usr/share/omarchy}/shell}
timeout_s=${SMOKE_TIMEOUT:-20}

dir=$(mktemp -d)
trap 'kill "${pid:-}" 2>/dev/null || true; [ -n "${SMOKE_KEEP:-}" ] && echo "kept $dir" >&2 || rm -rf "$dir"' EXIT

for entry in "$shell_root"/*; do
  [ "$(basename "$entry")" = shell.qml ] || ln -s "$entry" "$dir/$(basename "$entry")"
done
ln -s "$plugin" "$dir/omaslack"
cp "$plugin/tests/smoke/shell.qml" "$dir/shell.qml"

names=$(cd "$plugin/tests/smoke" && ls -- *Smoke.qml | paste -sd,)
log="$dir/smoke.log"
OMASLACK_SMOKE="$names" QT_QPA_PLATFORM=offscreen quickshell -p "$dir/shell.qml" >"$log" 2>&1 &
pid=$!

# quickshell keeps running after the harness is done; wait for its marker.
for _ in $(seq $((timeout_s * 10))); do
  grep -q "SMOKE DONE" "$log" && break
  kill -0 "$pid" 2>/dev/null || break
  sleep 0.1
done

grep -o "SMOKE \(PASS\|FAIL\).*" "$log" || true
if ! grep -q "SMOKE DONE" "$log"; then
  echo "smoke: the harness did not finish; quickshell log:" >&2
  cat "$log" >&2
  exit 1
fi
! grep -q "SMOKE FAIL" "$log"
