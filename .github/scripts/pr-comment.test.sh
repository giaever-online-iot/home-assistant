#!/usr/bin/env bash
# Unit tests for pr-comment.sh. No network.
set -uo pipefail  # no -e: keep all tests running even if earlier ones fail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PC="$HERE/pr-comment.sh"
fail=0

# render <wanted-csv> <published-csv> -> pr-comment.sh stdout for channel latest/edge/pr1
render() { SNAP_NAME=home-assistant bash "$PC" "latest/edge/pr1" "$1" "$2"; }

contains() { # desc haystack needle
  if printf '%s' "$2" | grep -qF -- "$3"; then
    printf 'ok   - %s\n' "$1"
  else
    printf 'FAIL - %s\n       missing: [%s]\n' "$1" "$3"
    fail=1
  fi
}
absent() { # desc haystack needle
  if printf '%s' "$2" | grep -qF -- "$3"; then
    printf 'FAIL - %s\n       unexpected: [%s]\n' "$1" "$3"
    fail=1
  else
    printf 'ok   - %s\n' "$1"
  fi
}

# All wanted arches published
out="$(render "amd64,arm64" "amd64,arm64")"
contains "full: success marker"     "$out" '✅ Published to'
contains "full: channel"            "$out" 'latest/edge/pr1'
contains "full: lists both arches"  "$out" 'amd64, arm64'
contains "full: install command"    "$out" 'sudo snap install home-assistant --channel=latest/edge/pr1'
absent   "full: no failed line"     "$out" '❌ Failed'

# Partial: only amd64 reached the channel
out="$(render "amd64,arm64" "amd64")"
contains "partial: warning marker"  "$out" '⚠️ Partial publish'
contains "partial: 1 of 2"          "$out" '1 of 2 architectures'
contains "partial: failed arm64"    "$out" '❌ Failed: arm64'
contains "partial: install command" "$out" 'sudo snap install home-assistant'

# Nothing published
out="$(render "amd64,arm64" "")"
contains "none: failure marker"     "$out" '❌ No architectures were published'
absent   "none: no install command" "$out" 'sudo snap install'

# Order-independent: published listed in reverse is still "complete"
out="$(render "amd64,arm64" "arm64,amd64")"
contains "reorder: success marker"  "$out" '✅ Published to'
absent   "reorder: no failed line"  "$out" '❌ Failed'

# SNAP_NAME default resolves to home-assistant even when empty
out="$(SNAP_NAME="" bash "$PC" "latest/edge/pr1" "amd64" "amd64")"
contains "default snap name"        "$out" 'sudo snap install home-assistant'

if [ "$fail" -eq 0 ]; then printf '\nAll pr-comment.sh tests passed.\n'; else printf '\nSome pr-comment.sh tests FAILED.\n'; fi
exit "$fail"
