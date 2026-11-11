#!/usr/bin/env bash
#
# Tests for scripts/release-gate.sh against a stubbed `gh api`, plus guards on
# the release workflow's pins. Offline: needs only bash, jq and grep.
#   test/release/gate_test.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GATE="$ROOT/scripts/release-gate.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
pass=0; failed=0

CSHA=1111111111111111111111111111111111111111
OTHER=2222222222222222222222222222222222222222
TAGOBJ=3333333333333333333333333333333333333333

# The stub answers by URL. Scenario knobs come from the environment:
#   STUB_TAG_TYPE   object type behind refs/tags/v1.2.3 (tag | commit)
#   STUB_MERGE_BASE merge base returned by the compare API
#   STUB_CHECKS     file holding the check_runs JSON array
cat >"$TMP/gh" <<'STUB'
#!/usr/bin/env bash
# args: api [--paginate] <path> [--jq <expr>]
args=("$@"); path=""; jqexpr="."; i=1
while (( i < ${#args[@]} )); do
  case "${args[$i]}" in
    --paginate) ;;
    --jq) i=$((i+1)); jqexpr="${args[$i]}" ;;
    *) path="${args[$i]}" ;;
  esac
  i=$((i+1))
done
case "$path" in
  repos/o/r/git/ref/tags/v1.2.3)
    [[ -n "${STUB_NO_TAG:-}" ]] && { echo "gh: Not Found (HTTP 404)" >&2; exit 1; }
    json="{\"object\":{\"type\":\"${STUB_TAG_TYPE:-tag}\",\"sha\":\"$TAGOBJ\"}}" ;;
  repos/o/r/git/tags/$TAGOBJ)
    json="{\"object\":{\"type\":\"commit\",\"sha\":\"$CSHA\"}}" ;;
  repos/o/r/compare/*)
    json="{\"merge_base_commit\":{\"sha\":\"${STUB_MERGE_BASE:-$CSHA}\"}}" ;;
  repos/o/r/commits/*/check-runs*)
    json="{\"check_runs\":$(cat "$STUB_CHECKS")}" ;;
  *) echo "stub: unexpected $path" >&2; exit 1 ;;
esac
jq -r "$jqexpr" <<<"$json"
STUB
chmod +x "$TMP/gh"

# check-run JSON: name:status:conclusion[:id[:run]]
checks() {
  local out="[" sep="" spec name status concl id run n=0
  for spec in "$@"; do
    IFS=: read -r name status concl id run <<<"$spec"
    n=$((n+1)); id="${id:-$n}"
    [[ "$concl" == "-" ]] && concl_json=null || concl_json="\"$concl\""
    out+="$sep{\"id\":$id,\"name\":\"$name\",\"status\":\"$status\",\"conclusion\":$concl_json,\"details_url\":\"https://github.com/o/r/actions/runs/${run:-1}/job/$id\"}"
    sep=","
  done
  echo "$out]"
}

GREEN=(a:completed:success b:completed:success)
run_gate() { # name expect_rc grep_pattern -- args...
  local name="$1" want="$2" pat="$3"; shift 4
  local out rc
  out="$(GH="$TMP/gh" GITHUB_REPOSITORY=o/r GATE_REQUIRED_CHECKS=$'a\nb' \
         GATE_WAIT_SECONDS="${WAIT:-0}" GATE_POLL_SECONDS=1 "$GATE" "$@" 2>&1)"; rc=$?
  if [[ $rc -eq $want ]] && grep -Eq -- "$pat" <<<"$out"; then
    pass=$((pass+1)); echo "ok   $name"
  else
    failed=$((failed+1)); echo "FAIL $name (rc=$rc want $want, pattern $pat)"
    # shellcheck disable=SC2001
    echo "$out" | sed 's/^/     /'
  fi
}
setchecks() { checks "$@" >"$TMP/checks.json"; export STUB_CHECKS="$TMP/checks.json"; }

export TAGOBJ CSHA
unset STUB_TAG_TYPE STUB_MERGE_BASE STUB_NO_TAG GITHUB_RUN_ID GATE_EXPECT_SHA

setchecks "${GREEN[@]}"
run_gate "green mainline tag passes" 0 "PASS $CSHA" -- v1.2.3
run_gate "dry run on a green mainline sha passes" 0 "PASS $CSHA" -- --sha "$CSHA"

STUB_MERGE_BASE=$OTHER run_gate "tag on a non-mainline sha stops" 1 "is not on master" -- v1.2.3
STUB_MERGE_BASE=$OTHER run_gate "dry run on a non-mainline sha stops" 1 "is not on master" -- --sha "$CSHA"
STUB_TAG_TYPE=commit run_gate "lightweight tag stops" 1 "lightweight tag" -- v1.2.3
STUB_NO_TAG=1 run_gate "missing tag stops" 1 "not found" -- v1.2.3
run_gate "non-release tag name stops" 1 "not a vX.Y.Z" -- release-1
run_gate "short sha stops" 1 "40-hex" -- --sha abc123
GATE_EXPECT_SHA=$OTHER run_gate "tag that disagrees with the run sha stops" 1 "peels to" -- v1.2.3

setchecks a:completed:success b:completed:failure
run_gate "red required check stops" 1 "b: failure" -- v1.2.3
setchecks a:completed:success b:completed:success c:completed:timed_out
run_gate "red non-required check stops" 1 "c: timed_out" -- v1.2.3
setchecks a:completed:success b:completed:success c:completed:skipped d:completed:neutral
run_gate "skipped and neutral extras pass" 0 "PASS" -- v1.2.3
setchecks a:completed:success
WAIT=0 run_gate "missing required check stops after the wait" 1 "b: missing" -- v1.2.3
setchecks a:completed:success b:in_progress:-
WAIT=0 run_gate "pending required check stops after the wait" 1 "b: in_progress" -- v1.2.3
setchecks a:completed:success b:completed:failure:5 b:completed:success:9
run_gate "a rerun that succeeded replaces the failed run" 0 "PASS" -- v1.2.3
setchecks a:completed:success b:completed:success:5 b:completed:failure:9
run_gate "a newer failed run replaces the green one" 1 "b: failure" -- v1.2.3

# The release run's own check-run (in progress, on the same CSHA) must not count.
setchecks a:completed:success b:completed:success gate:in_progress:-:7:777 release:completed:failure:8:777
GITHUB_RUN_ID=777 run_gate "the running workflow's own checks are ignored" 0 "PASS" -- v1.2.3
setchecks a:completed:success b:completed:success release:completed:failure:8:777
run_gate "without the run id its failed check counts" 1 "release: failure" -- v1.2.3

# Pending then green: the gate must wait, then pass.
setchecks a:completed:success b:queued:-
( sleep 2; checks a:completed:success b:completed:success >"$TMP/checks.json" ) &
WAIT=20 run_gate "waits for a pending check and then passes" 0 "waiting for: b \\(queued\\)" -- v1.2.3
wait

# Workflow pins: the release must not float.
REL="$ROOT/.github/workflows/release.yml"
guard() { # name pattern-that-must-be-absent
  if grep -Eq -- "$2" "$REL"; then failed=$((failed+1)); echo "FAIL $1"; grep -En -- "$2" "$REL" | sed 's/^/     /'
  else pass=$((pass+1)); echo "ok   $1"; fi
}
need() { # name pattern-that-must-be-present
  if grep -Eq -- "$2" "$REL"; then pass=$((pass+1)); echo "ok   $1"
  else failed=$((failed+1)); echo "FAIL $1 (missing: $2)"; fi
}
guard "release.yml has no check-latest: true" 'check-latest:[[:space:]]*true'
guard "release.yml has no floating Go (x or ~)" 'go-version:[[:space:]]*"?[0-9.]*(x|\*)'
guard "release.yml has no floating GoReleaser (~>)" '~>'
need  "release.yml pins Go exactly" 'GO_VERSION:[[:space:]]*"?1\.[0-9]+\.[0-9]+"?$'
need  "release.yml pins GoReleaser exactly" 'GORELEASER_VERSION:[[:space:]]*"?v2\.[0-9]+\.[0-9]+"?$'
need  "release.yml pins cosign exactly" 'COSIGN_VERSION:[[:space:]]*"?v[0-9]+\.[0-9]+\.[0-9]+"?$'
need  "release.yml pins syft exactly" 'SYFT_VERSION:[[:space:]]*"?v[0-9]+\.[0-9]+\.[0-9]+"?$'
need  "release.yml runs the gate before the release job" 'needs:[[:space:]]*gate'

# The dry run must use the same pins as the release.
DRY="$ROOT/.github/workflows/release-dryrun.yml"
for v in GO_VERSION GORELEASER_VERSION COSIGN_VERSION SYFT_VERSION; do
  a="$(grep -E "^  $v:" "$REL" | head -n1)"; b="$(grep -E "^  $v:" "$DRY" | head -n1)"
  if [[ -n "$a" && "$a" == "$b" ]]; then pass=$((pass+1)); echo "ok   release-dryrun.yml pins $v like release.yml"
  else failed=$((failed+1)); echo "FAIL $v differs: release.yml '$a' vs release-dryrun.yml '$b'"; fi
done
if grep -Eq 'check-latest:[[:space:]]*true|~>' "$DRY"; then failed=$((failed+1)); echo "FAIL release-dryrun.yml floats a tool"
else pass=$((pass+1)); echo "ok   release-dryrun.yml floats no tool"; fi

# One signing identity everywhere: the docs must name this very workflow file.
for doc in README.md SECURITY.md; do
  if grep -Fq '.github/workflows/release.yml@refs/tags/' "$ROOT/$doc"; then
    pass=$((pass+1)); echo "ok   $doc anchors the identity to release.yml@refs/tags/"
  else failed=$((failed+1)); echo "FAIL $doc does not anchor the identity to release.yml@refs/tags/"; fi
  if grep -Fq -- "--certificate-identity-regexp 'https://github.com/ftarasenko/koc/.*'" "$ROOT/$doc"; then
    failed=$((failed+1)); echo "FAIL $doc still has the any-workflow identity regexp"
  else pass=$((pass+1)); echo "ok   $doc has no any-workflow identity regexp"; fi
done

echo "release gate tests: $pass passed, $failed failed"
[[ $failed -eq 0 ]]
