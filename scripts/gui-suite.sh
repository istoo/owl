#!/usr/bin/env bash
# The one way to run the GUI suite (or any part of it) on a shared machine.
#
# The owner (11-16): "测试导致的系统非常的卡". A full run is 1,199 files,
# each starting its own Chromium or WebKit; at --test-concurrency=6 that is
# 3 to 5 cores and 4 to 5GB for an hour a half, and it was started at
# normal priority whatever else the machine was doing (another project's
# vitest had the load at 40 on 18 cores), and by more than one session at
# once. LESSONS.md asked for "a lock every suite goes through": it is
# scripts/heavy.sh, which every heavy run uses (one per machine, a load
# gate, low priority, its own process tree ended); see its notes.
#   - --test-concurrency defaults to min(2, cores/4) (GUI_SUITE_CONCURRENCY)
#   - Node 26's runner passes --heap-prof-interval to each file's process
#     as a default; no --heap-prof is on and no profile is written
#   - the log is kept whole in a file of its own (LESSONS: a suite's log is
#     kept whole until its reds are named), its summary printed at the end
#
# Usage: scripts/gui-suite.sh [files...]   (default: every *.test.cjs)
#        GUI_SUITE_HALF=1|2 runs the first or second half of the sorted list
# Files are names under internal/gui/tests. NODE_PATH must reach playwright.
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
# concurrency min(2, cores/4): each file is a browser of its own, and a
# machine others share wants room (ai-34, the Neox session on this
# machine, 11-16: load 74 on 18 cores). The lock, the load gate, the low
# priority and the clean-up are scripts/heavy.sh's, which every heavy run
# goes through (11-24: a load test beside a suite took the load to 77)
q=$(( cores / 4 )); conc=${GUI_SUITE_CONCURRENCY:-$(( q < 1 ? 1 : (q > 2 ? 2 : q) ))}
log=${GUI_SUITE_LOG:-"${TMPDIR:-/tmp}/owl-gui-suite-$(date +%Y%m%d-%H%M%S)-$$.log"}

echo "gui-suite: ${#files[@]} files, concurrency $conc, log $log" >&2
set +e
"$root/scripts/heavy.sh" node --test --test-concurrency="$conc" "${files[@]}" > "$log" 2>&1
code=$?
set -e
grep -E '^ℹ (tests|pass|fail|skipped|duration_ms)' "$log" || true
grep -E '^✖' "$log" | sed -E 's/ \([0-9.]+ms\)$//' | sort -u || true
echo "gui-suite: exit $code, log $log" >&2
exit $code
