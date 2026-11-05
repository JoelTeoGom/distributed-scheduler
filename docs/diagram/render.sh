#!/usr/bin/env bash
# Draws the architecture diagram and exports it to docs/distributed-scheduler.png at twice the size, with headless Chrome.
# Edit draw.mjs, then run ./render.sh. Needs Node and Google Chrome.
set -euo pipefail
cd "$(dirname "$0")"

node draw.mjs

chrome="${CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
work=$(mktemp -d)
trap 'kill "$chrome_pid" 2>/dev/null || true; wait "$chrome_pid" 2>/dev/null || true; rm -rf "$work"' EXIT

# The window is taller than the drawing because Chrome keeps part of it for itself.
"$chrome" --headless=new --disable-gpu --no-first-run --no-default-browser-check --disable-extensions \
  --user-data-dir="$work/profile" --hide-scrollbars \
  --force-device-scale-factor=2 --window-size=2040,1660 \
  --screenshot="$work/diagram.png" "file://$PWD/page.html" 2>/dev/null &
chrome_pid=$!

# On macOS headless Chrome can stay alive after writing the screenshot, so wait for the file instead of the process.
for _ in $(seq 1 60); do
  [ -s "$work/diagram.png" ] && break
  sleep 1
done
sleep 1
[ -s "$work/diagram.png" ] || { echo "Chrome wrote no screenshot" >&2; exit 1; }
mv "$work/diagram.png" ../distributed-scheduler.png
echo "wrote docs/distributed-scheduler.png"
