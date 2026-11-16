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
#     core count (GUI_SUITE_MAX_LOAD to change it), checked every 30s
#   - run at low priority (nice 10, and on macOS the utility QoS clamp), so
#     the user's own apps come first and the window never stalls
#   - --test-concurrency defaults to 4 (GUI_SUITE_CONCURRENCY)
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
max_load=${GUI_SUITE_MAX_LOAD:-$cores}
conc=${GUI_SUITE_CONCURRENCY:-4}
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
trap 'rm -rf "$lock"' EXIT INT TERM

# room first: what the user is doing comes before the suite
while awk -v l="$(load1)" -v m="$max_load" 'BEGIN { exit !(l >= m) }'; do
  echo "gui-suite: load $(load1) on $cores cores is at or over $max_load; waiting" >&2
  sleep 30
done

low=(nice -n 10)
if [ "$(uname)" = Darwin ] && command -v taskpolicy >/dev/null; then low=(taskpolicy -c utility nice -n 10); fi

echo "gui-suite: ${#files[@]} files, concurrency $conc, load $(load1), log $log" >&2
set +e
"${low[@]}" node --test --test-concurrency="$conc" "${files[@]}" > "$log" 2>&1
code=$?
set -e
grep -E '^ℹ (tests|pass|fail|skipped|duration_ms)' "$log" || true
grep -E '^✖' "$log" | sed -E 's/ \([0-9.]+ms\)$//' | sort -u || true
echo "gui-suite: exit $code, log $log" >&2
exit $code
