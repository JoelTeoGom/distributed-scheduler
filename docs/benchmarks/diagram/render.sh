#!/usr/bin/env bash
# Draws the benchmark platform and exports it to docs/benchmarks/platform.png at twice the size, with headless Chrome.
# Edit draw.mjs, then run ./render.sh. Needs Node and Chrome; in WSL it uses the Windows one.
set -euo pipefail
cd "$(dirname "$0")"

node draw.mjs

chrome="${CHROME:-/mnt/c/Program Files/Google/Chrome/Application/chrome.exe}"
work=$(mktemp -d)
# Windows Chrome cannot read files inside WSL reliably, so it works from a Windows temp folder.
if [[ "$chrome" == /mnt/* ]]; then
  work="$(wslpath "$(cmd.exe /c 'echo %TEMP%' 2>/dev/null | tr -d '\r')")/platform-render-$$"
  mkdir -p "$work"
fi
trap 'rm -rf "$work"' EXIT
cp platform.svg "$work/"
echo '<!doctype html><html><body style="margin:0;background:#fff"><img src="platform.svg" width="2040" height="1680" style="display:block"></body></html>' > "$work/page.html"

as_url() { if [[ "$chrome" == /mnt/* ]]; then echo "file:///$(wslpath -m "$1")"; else echo "file://$1"; fi; }
as_path() { if [[ "$chrome" == /mnt/* ]]; then wslpath -w "$1"; else echo "$1"; fi; }

# The window is taller than the drawing because Chrome keeps part of it for itself.
"$chrome" --headless=new --disable-gpu --no-first-run --no-default-browser-check --disable-extensions \
  --user-data-dir="$(as_path "$work/profile")" --hide-scrollbars \
  --force-device-scale-factor=2 --window-size=2040,1780 \
  --screenshot="$(as_path "$work/platform.png")" "$(as_url "$work/page.html")" >/dev/null 2>&1 || true

[ -s "$work/platform.png" ] || { echo "Chrome wrote no screenshot" >&2; exit 1; }
mv "$work/platform.png" ../platform.png
echo "wrote docs/benchmarks/platform.png"
