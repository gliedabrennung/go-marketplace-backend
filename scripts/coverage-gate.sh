#!/usr/bin/env bash
set -euo pipefail

if [[ $# -eq 0 ]]; then
  set -- coverage.out
fi

merged="$(mktemp)"
trap 'rm -f "$merged"' EXIT

awk '
  FNR == 1 && /^mode:/ { next }
  {
    key = $1
    statements[key] = $2
    if (!(key in hits) || $3 + 0 > hits[key] + 0) hits[key] = $3
  }
  END {
    for (key in statements) print key, statements[key], hits[key]
  }
' "$@" > "$merged"

layer_coverage() {
  local pattern="$1"
  awk -v pattern="$pattern" '
    $1 ~ pattern {
      total += $2
      if ($3 + 0 > 0) covered += $2
    }
    END {
      if (total == 0) { print "none"; exit }
      printf "%.1f", covered * 100 / total
    }
  ' "$merged"
}

check() {
  local name="$1" pattern="$2" threshold="$3"
  local value
  value="$(layer_coverage "$pattern")"
  if [[ "$value" == "none" ]]; then
    printf "%-12s no packages\n" "$name"
    return 0
  fi
  printf "%-12s %6s%% (min %s%%)\n" "$name" "$value" "$threshold"
  awk -v v="$value" -v t="$threshold" 'BEGIN { exit (v + 0 >= t + 0) ? 0 : 1 }'
}

status=0
check "domain" "/internal/[a-z]+/domain/" 90 || status=1
check "application" "/internal/[a-z]+/application/" 75 || status=1
check "total" "." 70 || status=1
exit "$status"
