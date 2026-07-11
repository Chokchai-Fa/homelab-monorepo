#!/usr/bin/env bash
#
# select-services.sh — decide which services to build and emit a GitHub Actions
# build matrix. Single source of truth is services.yaml.
#
# Modes (via EVENT_NAME):
#   workflow_dispatch -> build INPUT_SERVICES ("all" or comma-separated names)
#   push (default)    -> build services whose paths changed between BEFORE/AFTER
#
# Env inputs (all optional; sane local defaults):
#   MANIFEST         path to registry (default: services.yaml)
#   EVENT_NAME       "push" | "workflow_dispatch" (default: push)
#   INPUT_SERVICES   dispatch selection (default: all)
#   BEFORE_SHA       push event "before" sha
#   AFTER_SHA        push event "after" sha (default: HEAD)
#   GITHUB_OUTPUT    if set, writes matrix=/count= for the workflow
#
# Requires: yq (mikefarah), jq, git.
set -euo pipefail

MANIFEST="${MANIFEST:-services.yaml}"
EVENT_NAME="${EVENT_NAME:-push}"
INPUT_SERVICES="${INPUT_SERVICES:-all}"
BEFORE_SHA="${BEFORE_SHA:-}"
AFTER_SHA="${AFTER_SHA:-HEAD}"

command -v yq >/dev/null 2>&1 || { echo "error: yq (mikefarah) is required" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "error: jq is required" >&2; exit 1; }

ALL_JSON="$(yq -o=json -I=0 '.services' "$MANIFEST")"

# Convert a path glob (** and *) to an anchored regex for bash =~ matching.
glob_to_regex() {
  local g="$1"
  g="${g//./\\.}"                 # escape dots
  g="${g//\*\*/§DS§}"             # protect **
  g="${g//\*/[^/]*}"              # * -> any non-slash
  g="${g//§DS§/.*}"               # ** -> anything
  printf '^%s$' "$g"
}

selected_names=()

if [[ "$EVENT_NAME" == "workflow_dispatch" ]]; then
  if [[ -z "$INPUT_SERVICES" || "$INPUT_SERVICES" == "all" ]]; then
    mapfile -t selected_names < <(echo "$ALL_JSON" | jq -r '.[].name')
  else
    IFS=',' read -ra requested <<< "$INPUT_SERVICES"
    for r in "${requested[@]}"; do
      r="$(echo "$r" | xargs)"   # trim whitespace
      [[ -z "$r" ]] && continue
      if echo "$ALL_JSON" | jq -e --arg n "$r" 'any(.[]; .name==$n)' >/dev/null; then
        selected_names+=("$r")
      else
        echo "warning: unknown service '$r' ignored" >&2
      fi
    done
  fi
else
  # push: change detection
  if [[ -n "$BEFORE_SHA" && ! "$BEFORE_SHA" =~ ^0+$ ]]; then
    DIFF_BASE="$BEFORE_SHA"
  else
    # first push / no before sha: compare to previous commit if it exists,
    # else the empty tree (everything counts as changed).
    # Note: `--verify -q` stays silent on failure; plain `rev-parse HEAD~1`
    # would echo "HEAD~1" to stdout and corrupt DIFF_BASE.
    DIFF_BASE="$(git rev-parse --verify -q HEAD~1 || git hash-object -t tree /dev/null)"
  fi
  mapfile -t changed < <(git diff --name-only "$DIFF_BASE" "$AFTER_SHA")
  echo "Changed files (${#changed[@]}):" >&2
  printf '  %s\n' "${changed[@]:-}" >&2

  while IFS= read -r svc_b64; do
    svc="$(echo "$svc_b64" | base64 -d)"
    name="$(echo "$svc" | jq -r '.name')"
    matched=0
    while IFS= read -r glob; do
      re="$(glob_to_regex "$glob")"
      for f in "${changed[@]:-}"; do
        [[ -z "$f" ]] && continue
        if [[ "$f" =~ $re ]]; then matched=1; break; fi
      done
      [[ $matched -eq 1 ]] && break
    done < <(echo "$svc" | jq -r '.paths[]')
    [[ $matched -eq 1 ]] && selected_names+=("$name")
  done < <(echo "$ALL_JSON" | jq -r '.[] | @base64')
fi

# JSON array of selected names (empty-safe)
NAMES_JSON="$(printf '%s\n' "${selected_names[@]:-}" | jq -R . | jq -s 'map(select(length>0))')"

# Build the matrix "include" list: scalars only (+ rendered build-args string),
# dropping the object/array fields that GitHub matrix values cannot hold.
INCLUDE="$(echo "$ALL_JSON" | jq -c --argjson names "$NAMES_JSON" '
  [ .[]
    | select(.name as $n | $names | index($n))
    | . + { buildArgsStr: ((.buildArgs // {}) | to_entries | map("\(.key)=\(.value)") | join("\n")) }
    | del(.buildArgs, .paths)
  ]')"

MATRIX="$(jq -cn --argjson inc "$INCLUDE" '{include: $inc}')"
COUNT="$(echo "$INCLUDE" | jq 'length')"

echo "Selected services ($COUNT):" >&2
echo "$INCLUDE" | jq -r '.[].name' | sed 's/^/  /' >&2

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  {
    echo "matrix=$MATRIX"
    echo "count=$COUNT"
  } >> "$GITHUB_OUTPUT"
fi

echo "$MATRIX"
