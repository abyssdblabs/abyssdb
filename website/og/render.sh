#!/usr/bin/env bash
# Renders the Open Graph cards in website/public/og/ from og/card.html with
# headless Chrome. The PNGs are committed, so the website build needs no browser.
#
#   website/og/render.sh        # or: make og
#   CHROME=/path/to/chrome website/og/render.sh
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
out="$here/../public/og"
mkdir -p "$out"

if [[ -z "${CHROME:-}" ]]; then
	for c in "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" google-chrome google-chrome-stable chromium chromium-browser; do
		if command -v "$c" >/dev/null 2>&1 || [[ -x "$c" ]]; then
			CHROME="$c"
			break
		fi
	done
fi
if [[ -z "${CHROME:-}" ]]; then
	echo "error: Chrome not found (set CHROME)" >&2
	exit 1
fi

profile="$(mktemp -d)"
trap 'rm -rf "$profile"' EXIT

# render NAME PARAMS: PARAMS is the URL hash, e.g. "title=…&accent=…".
render() {
	"$CHROME" --headless=new --disable-gpu --hide-scrollbars --force-device-scale-factor=1 \
		--user-data-dir="$profile" --window-size=1200,630 --virtual-time-budget=4000 \
		--screenshot="$out/$1.png" "file://$here/card.html#$2" >/dev/null 2>&1
	echo "  og/$1.png"
}

enc() { python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1], safe=""))' "$1"; }

# card NAME EYEBROW TITLE ACCENT SUB STATS [SIZE]
card() {
	render "$1" "eyebrow=$(enc "$2")&title=$(enc "$3")&accent=$(enc "$4")&sub=$(enc "$5")&stats=$(enc "$6")&size=${7:-}"
}

echo "==> rendering Open Graph cards"
card default "The database at the edge of everything" \
	"Infinitely fast."$'\n'"Perfectly consistent." "Zero-knowledge." \
	"The AI-native, post-quantum, serverless, interplanetary SQL database." \
	"0.000 ms|p99.999 latency;∞|ops/sec;0 B|footprint" 64
card paper "New research · Technical Report AR-2026-001" \
	"The Empty Set" "Is All You Need." \
	"Simultaneous ACID, BASE and CAP guarantees, formally proven." \
	"17|theorems;33|references;1|state"
card investors "Investor relations · Series A" \
	"We're raising." "Obviously." \
	"The first database with a gross margin of exactly 100%." \
	"100%|gross margin;∞|runway;\$0.00|storage burn"
card playground "Playground · WebAssembly" \
	"AbyssDB," "in your browser." \
	"The complete database, compiled to WebAssembly. No server. No install." \
	""
card docs "Documentation" \
	"Zero learning curve." "Zero-knowledge." \
	"SQL reference, PostgreSQL compatibility, MCP, architecture and operations." \
	""
