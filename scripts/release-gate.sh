#!/usr/bin/env bash
#
# release-gate.sh — stop a release that is not a green commit on the mainline.
#
# Usage:
#   scripts/release-gate.sh <tag>        a release tag: annotated, peels to a SHA
#   scripts/release-gate.sh --sha <sha>  dry run for a commit with no tag yet
#
# The gate passes only when ALL of these hold for the commit the release builds:
#   1. (tag mode) the tag is a vX.Y.Z[-pre] ref and is annotated;
#   2. the commit is on the mainline: it is the tip of, or an ancestor of, the
#      release branch (compare API: merge base == the commit);
#   3. every required check-run exists on that exact SHA and concluded success
#      (a pending or missing one is waited for up to GATE_WAIT_SECONDS);
#   4. no other check-run on the SHA, bar the ones of the running workflow
#      itself, ended failure / timed_out / cancelled / action_required.
#
# It reads the GitHub API only (no checkout needed, no secrets, no writes), so
# it runs the same in a fork. Needs gh and jq; GH_TOKEN must carry
# contents:read and checks:read. It guards against a mistaken tag, not against
# someone who may push a tag AND edit the workflow: that boundary is the `release`
# environment and the tag ruleset (SECURITY.md).
#
# Environment:
#   GITHUB_REPOSITORY     owner/name (required)
#   RELEASE_BRANCH        mainline branch (default master)
#   GITHUB_RUN_ID         run to leave out of step 4 (the release run itself)
#   GATE_EXPECT_SHA       tag mode: the SHA the triggering event says (github.sha)
#   GATE_WAIT_SECONDS     how long to wait for pending checks (default 900)
#   GATE_POLL_SECONDS     poll interval (default 20)
#   GATE_REQUIRED_CHECKS  newline list of check-run names, replaces the default
#   GH                    gh command (tests put a stub here)
set -euo pipefail

GH="${GH:-gh}"
BRANCH="${RELEASE_BRANCH:-master}"
WAIT="${GATE_WAIT_SECONDS:-900}"
POLL="${GATE_POLL_SECONDS:-20}"
REPO="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"

# ci.yml build/test/lint matrix plus the vendor re-derivation: the checks that
# decide whether the commit is shippable. Keep in step with the job names there.
DEFAULT_REQUIRED='build-test
test-race
lint
crossbuild (linux, amd64)
crossbuild (linux, arm64)
crossbuild (darwin, amd64)
crossbuild (darwin, arm64)
crossbuild (windows, amd64)
crossbuild (windows, arm64)
vendor tree re-derives from upstream'
REQUIRED="${GATE_REQUIRED_CHECKS:-$DEFAULT_REQUIRED}"

fail() { echo "gate: STOP: $*" >&2; exit 1; }
note() { echo "gate: $*"; }

api() { "$GH" api "$@"; }

MODE="" ; TAG="" ; SHA=""
case "${1:-}" in
  --sha) MODE=sha; SHA="${2:?--sha needs a commit SHA}" ;;
  "")    echo "usage: $0 <tag> | --sha <sha>" >&2; exit 2 ;;
  *)     MODE=tag; TAG="$1" ;;
esac

if [[ "$MODE" == tag ]]; then
  [[ "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] \
    || fail "'$TAG' is not a vX.Y.Z release tag"
  ref="$(api "repos/$REPO/git/ref/tags/$TAG" --jq '[.object.type, .object.sha] | @tsv')" \
    || fail "tag $TAG not found in $REPO"
  otype="${ref%%$'\t'*}"; osha="${ref##*$'\t'}"
  [[ "$otype" == tag ]] \
    || fail "$TAG is a lightweight tag (points at a $otype); releases need an annotated tag: git tag -a $TAG -m $TAG"
  peeled="$(api "repos/$REPO/git/tags/$osha" --jq '[.object.type, .object.sha] | @tsv')" \
    || fail "cannot read the tag object of $TAG"
  [[ "${peeled%%$'\t'*}" == commit ]] || fail "$TAG does not point at a commit"
  SHA="${peeled##*$'\t'}"
  if [[ -n "${GATE_EXPECT_SHA:-}" && "$GATE_EXPECT_SHA" != "$SHA" ]]; then
    fail "tag $TAG peels to $SHA but the run is for $GATE_EXPECT_SHA"
  fi
  note "tag $TAG is annotated and points at $SHA"
else
  [[ "$SHA" =~ ^[0-9a-f]{40}$ ]] || fail "'$SHA' is not a full 40-hex commit SHA"
  note "dry run for commit $SHA (no tag)"
fi

# 2. mainline: with `compare base...head`, merge_base == head means head is
# already contained in base.
mb="$(api "repos/$REPO/compare/$BRANCH...$SHA" --jq .merge_base_commit.sha)" \
  || fail "cannot compare $SHA with $BRANCH"
[[ "$mb" == "$SHA" ]] \
  || fail "$SHA is not on $BRANCH (merge base is ${mb:0:12}); releases are cut from the mainline only"
note "$SHA is on $BRANCH"

# 3 and 4. check-runs on the exact SHA, newest run of each name wins.
evaluate() {
  local runs="$1"
  jq -r --arg required "$REQUIRED" --arg run "${GITHUB_RUN_ID:-}" '
    ($required | split("\n") | map(select(length > 0))) as $req
    | [ .[] | select($run == "" or ((.details_url // "") | contains("/actions/runs/" + $run + "/") | not)) ]
    | group_by(.name) | map(max_by(.id)) as $latest
    | ($latest | map({key: .name, value: .}) | from_entries) as $by
    | ( [ $latest[]
          | select(.status == "completed"
                   and (.conclusion | IN("failure","timed_out","cancelled","action_required","startup_failure","stale")))
          | "red\t" + .name + "\t" + .conclusion ]
      + [ $req[] | . as $n
          | ($by[$n]) as $c
          | if $c == null then "wait\t" + $n + "\tmissing"
            elif $c.status != "completed" then "wait\t" + $n + "\t" + $c.status
            elif $c.conclusion != "success" then "red\t" + $n + "\t" + $c.conclusion
            else "ok\t" + $n + "\tsuccess" end ] )
    | unique | .[]' <<<"$runs"
}

deadline=$(( $(date +%s) + WAIT ))
while :; do
  runs="$(api --paginate "repos/$REPO/commits/$SHA/check-runs?per_page=100" --jq '.check_runs[]' | jq -s .)" \
    || fail "cannot read check-runs of $SHA"
  verdict="$(evaluate "$runs")"
  red="$(awk -F'\t' '$1=="red"' <<<"$verdict")"
  if [[ -n "$red" ]]; then
    awk -F'\t' '$1=="red" {printf "gate:   %s: %s\n", $2, $3}' <<<"$red" >&2
    fail "CI on $SHA is not green"
  fi
  wait_="$(awk -F'\t' '$1=="wait"' <<<"$verdict")"
  [[ -z "$wait_" ]] && break
  if (( $(date +%s) >= deadline )); then
    awk -F'\t' '$1=="wait" {printf "gate:   %s: %s\n", $2, $3}' <<<"$wait_" >&2
    fail "required checks on $SHA did not finish within ${WAIT}s"
  fi
  note "waiting for: $(awk -F'\t' '$1=="wait" {printf "%s (%s); ", $2, $3}' <<<"$wait_")"
  sleep "$POLL"
done
awk -F'\t' '$1=="ok" {printf "gate:   %s: %s\n", $2, $3}' <<<"$verdict"
note "PASS $SHA${TAG:+ ($TAG)}: mainline, all required checks green"
