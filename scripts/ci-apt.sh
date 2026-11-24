#!/usr/bin/env bash
# ci-apt.sh installs Ubuntu packages on a CI runner, through a slow or
# broken mirror too.
#
# v0.2.36's Release (run 45350612221) failed in "Install GTK and WebKitGTK":
# the runner's mirror took minutes per package (libwebkit2gtk-4.1-0 alone
# over five) and the step's 15 minutes ran out. A rerun on another runner
# passed, but a fix made by hand is a fix for that run only (LESSONS). So:
#   - only the network is retried, for as long as CI_APT_BUDGET_SECS (1080)
#     allows: each try updates (CI_APT_UPDATE_SECS, 180) and downloads the
#     packages (--download-only, CI_APT_FETCH_SECS, 420), a stalled
#     connection giving up after 30s (Acquire::Retries). What a try
#     downloaded stays in apt's cache, so the next goes on from there: a
#     try that lost a few packages to a flaky link is progress, not a loss
#   - the tries take turns between Ubuntu's own archive (archive.ubuntu.com,
#     or ports.ubuntu.com for arm64) and the runner's mirror, starting with
#     the runner's
#   - the install itself reads apt's cache only and is not cut short: an
#     install killed half-way had to start again each try (412 packages on
#     a slow disk ran out three 420s tries in a row); the step's own
#     timeout-minutes, over the budget, bounds it
#
# Usage: scripts/ci-apt.sh <package>...
set -uo pipefail

[ $# -gt 0 ] || { echo "usage: scripts/ci-apt.sh <package>..." >&2; exit 64; }
sudo=(); [ "$(id -u)" = 0 ] || sudo=(sudo)
opts=(-o Acquire::Retries=3 -o Acquire::http::Timeout=30 -o Acquire::https::Timeout=30 -o DPkg::Lock::Timeout=120)
update_secs=${CI_APT_UPDATE_SECS:-180}
fetch_secs=${CI_APT_FETCH_SECS:-420}
budget=${CI_APT_BUDGET_SECS:-1080}
export DEBIAN_FRONTEND=noninteractive

sources=()
for f in /etc/apt/sources.list /etc/apt/sources.list.d/*.sources /etc/apt/sources.list.d/*.list; do
  [ -f "$f" ] && sources+=("$f")
done
saved=$(mktemp -d)
for f in "${sources[@]}"; do cp "$f" "$saved/$(echo "$f" | tr / _)"; done

# Ubuntu's own archive for every mirror line, ports for the arm64 one
use_archive() {
  for f in "${sources[@]}"; do
    "${sudo[@]}" sed -E -i \
      -e 's#(https?://)[^ /]+/ubuntu-ports/?#http://ports.ubuntu.com/ubuntu-ports/#g' \
      -e 's#(https?://)[^ /]+/ubuntu/?( |$)#http://archive.ubuntu.com/ubuntu/\2#g' "$f"
  done
}
use_runners() {
  for f in "${sources[@]}"; do "${sudo[@]}" cp "$saved/$(echo "$f" | tr / _)" "$f"; done
}

left() { echo $(( deadline - $(date +%s) )); }
# at least 1: timeout 0 would mean no bound at all
bounded() { local l; l=$(left); [ "$l" -gt "$1" ] && l=$1; [ "$l" -lt 1 ] && l=1; echo "$l"; }
deadline=$(( $(date +%s) + budget ))
try=0
while [ "$(left)" -gt 0 ]; do
  try=$((try + 1))
  if [ $((try % 2)) = 0 ]; then
    echo "ci-apt: try $try, from Ubuntu's own archive" >&2; use_archive
  elif [ "$try" -gt 1 ]; then
    echo "ci-apt: try $try, from the runner's mirror" >&2; use_runners
  fi
  start=$(date +%s)
  if timeout "$(bounded "$update_secs")" "${sudo[@]}" apt-get "${opts[@]}" -q update &&
     timeout "$(bounded "$fetch_secs")" "${sudo[@]}" apt-get "${opts[@]}" -q install -y --download-only "$@"; then
    echo "ci-apt: downloaded on try $try in $(( $(date +%s) - start ))s" >&2
    # the sources stay as this try had them: apt's lists are that mirror's
    # now, and a later apt-get in the job reads them as they are
    start=$(date +%s)
    # from the cache only; a package missing from it fails here, not
    # fetched again from a mirror that may be the slow one
    "${sudo[@]}" apt-get "${opts[@]}" -q install -y --no-download "$@" || exit 1
    echo "ci-apt: installed in $(( $(date +%s) - start ))s" >&2
    exit 0
  fi
  echo "ci-apt: try $try failed after $(( $(date +%s) - start ))s" >&2
  sleep 2
done
use_runners
echo "ci-apt: not downloaded within ${budget}s ($try tries)" >&2
exit 1
