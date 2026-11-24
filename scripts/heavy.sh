#!/usr/bin/env bash
# heavy.sh runs one heavy command on a shared machine: a GUI suite, a whole
# `go test ./...`, a load test, a benchmark, a long measurement.
#
# scripts/gui-suite.sh had the lock, the load gate and the low priority, so
# two suites never ran at once; but a load test or a benchmark went around
# it. On 11-24 a memory experiment's load generator (about 2.5 cores) ran
# beside a GUI suite and the load reached 77 on 18 cores. Every heavy run
# now goes through here, the GUI suite too:
#
#   - one heavy run per machine: a lock in $TMPDIR, held by a live pid;
#     another run waits for it (or, with HEAVY_NOWAIT=1, gives up at once,
#     exit 75)
#   - started only once the machine has room: the 1-minute load under the
#     core count and under 20 (HEAVY_MAX_LOAD) for three looks in a row,
#     30s apart (HEAVY_CALM)
#   - run at low priority (nice 10, and on macOS the utility QoS clamp)
#   - whatever ends it (its end, Ctrl-C, a kill) ends every process it
#     started, and only those: its own process tree, by pid
# debt: Playwright's WebKit renders in XPC services that launchd starts,
# not this tree, so they keep normal priority and outlive no clean-up of
# ours; a renice of them would need to tell one run's from another's
# debt: the load is looked at before the run only; a run crowded after it
# starts goes on
#
# Usage: scripts/heavy.sh <command> [args...]
# Stop a run with Ctrl-C, or kill -TERM its pid: a script started in the
# background has SIGINT ignored from its start, which no trap undoes.
# The GUI_SUITE_* names of these settings are read too, for older notes.
set -euo pipefail

[ $# -gt 0 ] || { echo "usage: scripts/heavy.sh <command> [args...]" >&2; exit 64; }

cores=$(getconf _NPROCESSORS_ONLN 2>/dev/null || sysctl -n hw.ncpu)
max_load=${HEAVY_MAX_LOAD:-${GUI_SUITE_MAX_LOAD:-$(( cores < 20 ? cores : 20 ))}}
calm_looks=${HEAVY_CALM:-${GUI_SUITE_CALM:-3}}
nowait=${HEAVY_NOWAIT:-${GUI_SUITE_NOWAIT:-}}
# the GUI suite's lock, kept at its name: notes and gates read it there
lock="${TMPDIR:-/tmp}/owl-gui-suite.lock"

load1() { uptime | sed -E 's/.*load averages?: *([0-9.]+).*/\1/'; }

# one heavy run per machine: mkdir is atomic; a lock whose pid is gone is
# stale
while ! mkdir "$lock" 2>/dev/null; do
  holder=$(cat "$lock/pid" 2>/dev/null || true)
  if [ -n "$holder" ] && ! kill -0 "$holder" 2>/dev/null; then
    rm -rf "$lock"; continue
  fi
  if [ "$nowait" = 1 ]; then echo "heavy: another run holds the machine (pid ${holder:-?}); not started" >&2; exit 75; fi
  echo "heavy: waiting for the run in pid ${holder:-?} to end" >&2
  sleep 30
done
echo $$ > "$lock/pid"
tree() { local c; for c in $(pgrep -P "$1" 2>/dev/null); do tree "$c"; echo "$c"; done; }
runner=""
finish() {
  if [ -n "$runner" ]; then
    # INT first, as a Ctrl-C would (node --test closes its files' browsers
    # on it); a command that has INT ignored, as one started from a
    # background script inherits, gets TERM after 2s instead of the whole
    # wait. Its tree goes last either way.
    kill -INT "$runner" 2>/dev/null || true
    for i in 1 2 3 4 5 6 7 8 9 10; do
      kill -0 "$runner" 2>/dev/null || break
      [ "$i" = 3 ] && kill -TERM "$runner" 2>/dev/null
      sleep 1
    done
  fi
  local left; left=$(tree $$)
  [ -n "$left" ] && kill $left 2>/dev/null || true
  rm -rf "$lock"
}
trap finish EXIT
trap 'exit 130' INT TERM

# room first: three looks in a row under the bar, 30s apart; one look at
# a dip started a run at 16.6 that the load then took to 72 (11-16)
calm=0
while [ "$calm" -lt "$calm_looks" ]; do
  if awk -v l="$(load1)" -v m="$max_load" 'BEGIN { exit !(l < m) }'; then calm=$((calm + 1)); else
    calm=0; echo "heavy: load $(load1) on $cores cores is at or over $max_load; waiting" >&2
  fi
  [ "$calm" -lt "$calm_looks" ] && sleep 30
done

low=(nice -n 10)
if [ "$(uname)" = Darwin ] && command -v taskpolicy >/dev/null; then low=(taskpolicy -c utility nice -n 10); fi

echo "heavy: $1, load $(load1)" >&2
set +e
# job control on for the start: an async command of a script that has it
# off starts with SIGINT ignored, so the INT finish forwards never reached
# it and every stop waited the whole 10s for the tree kill. In a group of
# its own, the command hears that INT and ends as it would at a Ctrl-C.
set -m
"${low[@]}" "$@" &
runner=$!
set +m
wait "$runner"
code=$?
runner=""
set -e
exit $code
