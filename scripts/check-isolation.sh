#!/usr/bin/env bash
# Isolation guard (see docs/ADAPTER-ISOLATION.md).
#
# Two invariants are enforced:
#
# 1. ADAPTER isolation — a concrete adapter package may be imported ONLY from:
#      a. its own package directory, and
#      b. its CLI's cmd/<tool>/register.go — and ONLY that tool's register.go.
#
# 2. CLI isolation — each CLI is standalone with zero coupling to its siblings.
#    Code under cmd/<cli>/ or internal/<cli>/ may import internal/<cli>/**,
#    internal/core/**, and its own adapters — but NEVER another CLI's
#    internal/<otherCli>/**.
#
# Any violation fails the build.
set -euo pipefail

# The set of standalone CLIs (each owns cmd/<cli> and internal/<cli>).
# tcli is contract-only: it orchestrates the others via subprocess + JSON envelope
# and must NEVER import a sibling's internal package — this guard enforces that.
CLIS="apicli dbcli jcli logcli tcli"

# adapters/<category>/<name>  ->  owning CLI tool.
category_tool() {
  case "$1" in
    log)            echo logcli ;;
    db)             echo dbcli ;;
    deploy|rollout) echo jcli ;;
    api)            echo apicli ;;
    *)              echo "" ;;   # unknown category -> no register.go is allowed
  esac
}

fail=0
for dir in adapters/*/*/; do
  [ -d "$dir" ] || continue
  name=$(basename "$dir")                       # e.g. local_file
  category=$(basename "$(dirname "$dir")")      # e.g. log
  import="adapters/$category/$name"             # e.g. adapters/log/local_file
  tool=$(category_tool "$category")
  allowed_register="cmd/$tool/register.go"

  hits=$(grep -rl "$import" --include='*.go' . | grep -v "^\./$dir" || true)
  while IFS= read -r f; do
    [ -z "$f" ] && continue
    if [ -n "$tool" ] && [ "$f" = "./$allowed_register" ]; then
      continue   # sanctioned: this tool's own register.go
    fi
    echo "ISOLATION VIOLATION: $import imported by $f (only $dir and ./$allowed_register may)"
    fail=1
  done <<< "$hits"
done

[ "$fail" -eq 0 ] && echo "OK: adapter isolation clean"

# --- 2. CLI isolation: no CLI may import a sibling CLI's internal package. ---
module=$(awk 'NR==1{print $2}' go.mod)   # e.g. github.com/no-today/aidev-clis
for cli in $CLIS; do
  for other in $CLIS; do
    [ "$other" = "$cli" ] && continue
    import="$module/internal/$other"      # the forbidden sibling package root
    # Search only this CLI's own source trees.
    hits=$(grep -rl "$import" --include='*.go' "cmd/$cli" "internal/$cli" 2>/dev/null || true)
    while IFS= read -r f; do
      [ -z "$f" ] && continue
      echo "ISOLATION VIOLATION: $cli imports sibling internal/$other in $f (CLIs must be standalone)"
      fail=1
    done <<< "$hits"
  done
done

[ "$fail" -eq 0 ] && echo "OK: cli isolation clean"
exit $fail
