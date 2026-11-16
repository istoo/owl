#!/usr/bin/env bash
# The one way to run the GUI suite (or any part of it) on a shared machine.
#
# The owner (11-16): "测试导致的系统非常的卡". A full run is 1,199 files,
# each starting its own Chromium or WebKit; at --test-concurrency=6 that is
# 3 to 5 cores and 4 to 5GB for an hour a half, and it was started at
# normal priority whatever else the machine was doing (another project's
# vitest had the load at 40 on 18 cores), and by more than one session at
# once. LESSONS.md asked for "a lock every suite goes through"; this is it:
#
#   - one suite per machine: a lock in $TMPDIR, held by a live pid; another
#     run waits for it (or, with GUI_SUITE_NOWAIT=1, gives up at once)
#   - started only once the machine has room: the 1-minute load under the
#     core count and under 20 (GUI_SUITE_MAX_LOAD) for three looks in a
#     row, 30s apart (GUI_SUITE_CALM)
#   - run at low priority (nice 10, and on macOS the utility QoS clamp), so
#     the user's own apps come first and the window never stalls
#   - --test-concurrency defaults to min(2, cores/4) (GUI_SUITE_CONCURRENCY)
#   - Node 26's runner passes --heap-prof-interval to each file's process
#     as a default; no --heap-prof is on and no profile is written
# debt: the load is looked at before the run only; a run that starts in
# room and is then crowded goes on (pausing node --test mid-run would leave
# browsers half-way through a file)
#   - the log is kept whole in a file of its own (LESSONS: a suite's log is
#     kept whole until its reds are named), its summary printed at the end
#
# Usage: scripts/gui-suite.sh [files...]   (default: every *.test.cjs)
#        GUI_SUITE_HALF=1|2 runs the first or second half of the sorted list
# Files are names under internal/gui/tests. NODE_PATH must reach playwright.
# debt: the lock and the load gate are per machine; two machines sharing a
# repository folder on a network disk are not covered (none do today).
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
tests="$root/internal/gui/tests"
cd "$tests"

if [ $# -gt 0 ]; then files=("$@"); else
  files=(); while IFS= read -r f; do files+=("$f"); done < <(ls *.test.cjs | sort)
  n=${#files[@]}; half=$(( (n + 1) / 2 ))
  case "${GUI_SUITE_HALF:-}" in
    1) files=("${files[@]:0:half}") ;;
    2) files=("${files[@]:half}") ;;
  esac
fi

cores=$(getconf _NPROCESSORS_ONLN 2>/dev/null || sysctl -n hw.ncpu)
# room is the core count, never over 20; concurrency min(2, cores/4): each
# file is a browser of its own, and a machine others share wants room
# (ai-34, the Neox session on this machine, 11-16: load 74 on 18 cores)
max_load=${GUI_SUITE_MAX_LOAD:-$(( cores < 20 ? cores : 20 ))}
q=$(( cores / 4 )); conc=${GUI_SUITE_CONCURRENCY:-$(( q < 1 ? 1 : (q > 2 ? 2 : q) ))}
lock="${TMPDIR:-/tmp}/owl-gui-suite.lock"
log=${GUI_SUITE_LOG:-"${TMPDIR:-/tmp}/owl-gui-suite-$(date +%Y%m%d-%H%M%S)-$$.log"}

load1() { uptime | sed -E 's/.*load averages?: *([0-9.]+).*/\1/'; }

# one suite per machine: mkdir is atomic; a lock whose pid is gone is stale
while ! mkdir "$lock" 2>/dev/null; do
  holder=$(cat "$lock/pid" 2>/dev/null || true)
  if [ -n "$holder" ] && ! kill -0 "$holder" 2>/dev/null; then
    rm -rf "$lock"; continue
  fi
  if [ "${GUI_SUITE_NOWAIT:-}" = 1 ]; then echo "gui-suite: another run holds the machine (pid ${holder:-?}); not started" >&2; exit 75; fi
  echo "gui-suite: waiting for the run in pid ${holder:-?} to end" >&2
  sleep 30
done
echo $$ > "$lock/pid"
# whatever ends the run (its end, Ctrl-C, a kill) ends every browser it
# started, and only those: its own process tree, by pid. A run stopped
# with SIGINT left 13 Playwright processes, and clearing them by name
# (pkill -f ms-playwright) also closed another session's browsers (11-16)
tree() { local c; for c in $(pgrep -P "$1" 2>/dev/null); do tree "$c"; echo "$c"; done; }
runner=""
finish() {
  if [ -n "$runner" ]; then
    kill -INT "$runner" 2>/dev/null || true
    for _ in 1 2 3 4 5 6 7 8 9 10; do kill -0 "$runner" 2>/dev/null || break; sleep 1; done
  fi
  local left; left=$(tree $$)
  [ -n "$left" ] && kill $left 2>/dev/null || true
  rm -rf "$lock"
}
trap finish EXIT
trap 'exit 130' INT TERM

# room first: what the user is doing comes before the suite. Three looks
# in a row under the bar, 30s apart: one look at a dip started a run at
# 16.6 that the load then took to 72 (11-16)
calm=0
while [ "$calm" -lt "${GUI_SUITE_CALM:-3}" ]; do
  if awk -v l="$(load1)" -v m="$max_load" 'BEGIN { exit !(l < m) }'; then calm=$((calm + 1)); else
    calm=0; echo "gui-suite: load $(load1) on $cores cores is at or over $max_load; waiting" >&2
  fi
  [ "$calm" -lt "${GUI_SUITE_CALM:-3}" ] && sleep 30
done

low=(nice -n 10)
if [ "$(uname)" = Darwin ] && command -v taskpolicy >/dev/null; then low=(taskpolicy -c utility nice -n 10); fi

echo "gui-suite: ${#files[@]} files, concurrency $conc, load $(load1), log $log" >&2
set +e
"${low[@]}" node --test --test-concurrency="$conc" "${files[@]}" > "$log" 2>&1 &
runner=$!
wait "$runner"
code=$?
runner=""
set -e
grep -E '^ℹ (tests|pass|fail|skipped|duration_ms)' "$log" || true
grep -E '^✖' "$log" | sed -E 's/ \([0-9.]+ms\)$//' | sort -u || true
echo "gui-suite: exit $code, log $log" >&2
exit $code
