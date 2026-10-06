#!/usr/bin/env bash
# Runs pgbench against a fresh AbyssDB server in every protocol mode.
#
#   scripts/pgbench/run.sh                     # or: make pgbench
#   DURATION=30 CLIENTS=16 JOBS=8 scripts/pgbench/run.sh
#   MODES=prepared ARGS="--shards 4 --nodes 3" scripts/pgbench/run.sh
#   PIPELINE=1 scripts/pgbench/run.sh          # one round trip per transaction
#   TRANSPORT=tcp scripts/pgbench/run.sh       # TCP instead of a Unix socket
#
# Environment:
#   DURATION  seconds per mode (default 5)
#   CLIENTS   concurrent clients, pgbench -c (default 8)
#   JOBS      pgbench threads, pgbench -j (default 4)
#   MODES     protocol modes to run (default "simple extended prepared")
#   PORT      PostgreSQL port for the server (default 55432)
#   ARGS      extra flags for `abyssdb serve` (e.g. --shards 4 --nodes 3)
#   TRANSPORT unix (default; a Unix-domain socket, like a local psql) or tcp
#   PIPELINE  1 to send each transaction in one round trip (pipeline mode;
#             extended and prepared modes only)
#   ABYSSDB   a prebuilt abyssdb binary to use instead of building one
#   PGBENCH   pgbench binary (default: pgbench, or the newest pgbench-NN)
#   PSQL      psql binary (default: psql, or the newest psql-NN)
#
# Exits non-zero if any transaction fails.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"

DURATION="${DURATION:-5}"
CLIENTS="${CLIENTS:-8}"
JOBS="${JOBS:-4}"
PORT="${PORT:-55432}"
ARGS="${ARGS:-}"
TRANSPORT="${TRANSPORT:-unix}"
PIPELINE="${PIPELINE:-0}"
script="$here/tpcb-like.sql"
if [[ "$PIPELINE" == "1" ]]; then
	script="$here/tpcb-like-pipelined.sql"
	MODES="${MODES:-extended prepared}" # pgbench pipelines need the extended protocol
fi
MODES="${MODES:-simple extended prepared}"

# find_tool pgbench → "pgbench", or the highest-versioned "pgbench-NN" on PATH.
find_tool() {
	if command -v "$1" >/dev/null 2>&1; then
		echo "$1"
		return
	fi
	local best
	best="$(compgen -c "$1-" | grep -E "^$1-[0-9]+$" | sort -t- -k2 -n | tail -1 || true)"
	if [[ -z "$best" ]]; then
		echo "error: $1 not found on PATH (set $(echo "$1" | tr a-z A-Z) to its location)" >&2
		exit 1
	fi
	echo "$best"
}
PGBENCH="${PGBENCH:-$(find_tool pgbench)}"
PSQL="${PSQL:-$(find_tool psql)}"

# A short path: Unix socket paths are limited to about 104 bytes.
work="$(mktemp -d /tmp/abyss-pgbench.XXXXXX)"
server=""
cleanup() {
	if [[ -n "$server" ]]; then
		kill "$server" 2>/dev/null || true
		wait "$server" 2>/dev/null || true
	fi
	rm -rf "$work"
}
trap cleanup EXIT

abyssdb="${ABYSSDB:-}"
if [[ -z "$abyssdb" ]]; then
	echo "==> building abyssdb"
	(cd "$root" && go build -o "$work/abyssdb" ./cmd/abyssdb)
	abyssdb="$work/abyssdb"
fi

case "$TRANSPORT" in
unix) host="$work" ;;
tcp) host=127.0.0.1 ;;
*)
	echo "error: TRANSPORT must be unix or tcp" >&2
	exit 1
	;;
esac
echo "==> starting server ($TRANSPORT, port $PORT${ARGS:+, $ARGS})"
# shellcheck disable=SC2086 # ARGS is a list of flags
"$abyssdb" serve $ARGS --pg-addr "127.0.0.1:$PORT" --pg-socket-dir "$work" --awp-addr "" --metrics-addr "" \
	>"$work/server.log" 2>&1 &
server=$!

export PGHOST="$host" PGPORT="$PORT" PGUSER=nobody PGDATABASE=abyss PGPASSWORD=nobody
for _ in $(seq 50); do
	"$PSQL" -qAt -c "SELECT 1" >/dev/null 2>&1 && break
	if ! kill -0 "$server" 2>/dev/null; then
		echo "error: server exited:" >&2
		cat "$work/server.log" >&2
		exit 1
	fi
	sleep 0.1
done

echo "==> declaring pgbench tables"
"$PSQL" -q -v ON_ERROR_STOP=1 -f "$here/schema.sql" 2>&1 | grep -v '^NOTICE:' || true

pipelined=""
[[ "$PIPELINE" == "1" ]] && pipelined=", pipelined"
echo "==> running $("$PGBENCH" --version | head -1): ${DURATION}s per mode, $CLIENTS clients, $JOBS threads$pipelined"
printf '\n%-10s %14s %10s %12s\n' "mode" "transactions" "failed" "tps"
status=0
for mode in $MODES; do
	out="$work/pgbench-$mode.txt"
	if ! "$PGBENCH" -n -M "$mode" -c "$CLIENTS" -j "$JOBS" -T "$DURATION" -f "$script" >"$out" 2>&1; then
		status=1
	fi
	txns="$(sed -n 's/^number of transactions actually processed: \([0-9]*\).*/\1/p' "$out")"
	failed="$(sed -n 's/^number of failed transactions: \([0-9]*\).*/\1/p' "$out")"
	tps="$(sed -n 's/^tps = \([0-9.]*\).*/\1/p' "$out")"
	printf '%-10s %14s %10s %12s\n' "$mode" "${txns:-?}" "${failed:-?}" "${tps:-?}"
	if [[ "${failed:-1}" != "0" ]]; then
		status=1
		grep -iE 'error|abort' "$out" | head -5 >&2 || true
	fi
done
echo
if [[ $status -eq 0 ]]; then
	echo "All transactions committed. Every one in 0.000 ms."
else
	echo "pgbench reported failures (see above). Server log:" >&2
	tail -40 "$work/server.log" >&2
fi
exit $status
