#!/usr/bin/env bash
# Benchmark gate for CI: runs pgbench against a build and, if given, compares
# it with a base build, interleaving the runs to even out noisy machines.
#
#   scripts/pgbench/ci.sh HEAD_BINARY [BASE_BINARY]
#
# Fails if any transaction fails, if pgbench -i fails, or if the head build's
# median throughput is more than TOLERANCE percent below the base build's.
# Writes a Markdown report to $GITHUB_STEP_SUMMARY when set.
#
# Environment: ROUNDS (default 3), DURATION seconds per run (default 5),
# TOLERANCE percent (default 10), PGBENCH and PSQL as for run.sh.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
head_bin="$1"
base_bin="${2:-}"
ROUNDS="${ROUNDS:-3}"
DURATION="${DURATION:-5}"
TOLERANCE="${TOLERANCE:-10}"
summary="${GITHUB_STEP_SUMMARY:-/dev/null}"

# tps BINARY: one prepared-mode run; prints transactions per second.
tps() {
	local out
	if ! out="$(ABYSSDB="$1" MODES=prepared DURATION="$DURATION" "$here/run.sh" 2>&1)"; then
		echo "$out" >&2
		echo "error: pgbench reported failures with $1" >&2
		exit 1
	fi
	awk '/^prepared/ { printf "%d\n", $4 }' <<<"$out"
}

median() { tr ' ' '\n' | grep -v '^$' | sort -n | awk '{ a[NR] = $1 } END { print a[int((NR + 1) / 2)] }'; }

head_runs=""
base_runs=""
for i in $(seq "$ROUNDS"); do
	echo "==> round $i of $ROUNDS"
	head_runs+="$(tps "$head_bin") "
	if [[ -n "$base_bin" ]]; then
		base_runs+="$(tps "$base_bin") "
	fi
done
head_median="$(echo "$head_runs" | median)"
echo "head: $head_runs(median $head_median tps)"

{
	echo "## pgbench"
	echo
	echo "TPC-B-like, prepared, 8 clients, Unix socket, ${DURATION}s per run, median of $ROUNDS."
	echo
	echo "| Build | Runs (tps) | Median |"
	echo "|---|---|---:|"
	echo "| this change | $head_runs | **$head_median** |"
} >>"$summary"

status=0
if [[ -n "$base_bin" ]]; then
	base_median="$(echo "$base_runs" | median)"
	echo "base: $base_runs(median $base_median tps)"
	change="$(awk -v h="$head_median" -v b="$base_median" 'BEGIN { printf "%+.1f", (h - b) * 100 / b }')"
	echo "| base | $base_runs | $base_median |" >>"$summary"
	echo >>"$summary"
	if awk -v h="$head_median" -v b="$base_median" -v t="$TOLERANCE" 'BEGIN { exit !(h < b * (100 - t) / 100) }'; then
		echo "error: throughput dropped ${change}% (tolerance ${TOLERANCE}%)" >&2
		echo "❌ Throughput changed by **${change}%**, beyond the ${TOLERANCE}% tolerance." >>"$summary"
		status=1
	else
		echo "throughput change: ${change}%"
		echo "✅ Throughput changed by ${change}% (tolerance ${TOLERANCE}%). No failed transactions." >>"$summary"
	fi
else
	echo >>"$summary"
	echo "✅ No failed transactions." >>"$summary"
fi

# pgbench -i must keep working.
echo "==> pgbench -i"
# Like run.sh: the plain name, or the newest versioned binary (pgbench-18).
find_tool() {
	if command -v "$1" >/dev/null 2>&1; then
		echo "$1"
	else
		compgen -c "$1-" | grep -E "^$1-[0-9]+$" | sort -t- -k2 -n | tail -1
	fi
}
PSQL="${PSQL:-$(find_tool psql)}"
PGBENCH="${PGBENCH:-$(find_tool pgbench)}"
sock="$(mktemp -d /tmp/abyss-ci.XXXXXX)"
"$head_bin" serve --pg-addr 127.0.0.1:55433 --pg-socket-dir "$sock" --awp-addr "" --metrics-addr "" >"$sock/server.log" 2>&1 &
server=$!
trap 'kill $server 2>/dev/null || true; rm -rf "$sock"' EXIT
for _ in $(seq 50); do
	"$PSQL" -h "$sock" -p 55433 -U nobody -qAt -c "SELECT 1" abyss >/dev/null 2>&1 && break
	sleep 0.1
done
if init="$(PGPASSWORD=nobody "$PGBENCH" -h "$sock" -p 55433 -U nobody -i -q -s 1 abyss 2>&1)"; then
	done_line="$(grep '^done in' <<<"$init")"
	echo "$done_line"
	echo "✅ \`pgbench -i -s 1\`: $done_line" >>"$summary"
else
	echo "$init" >&2
	echo "error: pgbench -i failed" >&2
	echo "❌ \`pgbench -i\` failed." >>"$summary"
	status=1
fi
exit $status
