#!/usr/bin/env bash
#
# release-inputs.sh — print the inputs of this build as a Markdown section for the
# release notes, so a later rebuild can start from the same tools.
#
# Usage: scripts/release-inputs.sh <tag>
# Run in the checkout being built, after Go, cosign and syft are installed. The
# *_VERSION variables are the pins from the workflow; the tool outputs below them
# are what actually ran. A mismatch between the two is a bug in the workflow.
set -euo pipefail

TAG="${1:?tag required}"

tool() { # name, command...: first line of the output, or "not on PATH"
  local name="$1"; shift
  if command -v "$1" >/dev/null 2>&1; then "$@" 2>&1 | head -n 1 || true; else echo "$name not on PATH"; fi
}
sum() { if [[ -f "$1" ]]; then sha256sum "$1" | cut -d' ' -f1; else echo "absent"; fi; }

cat <<MD

### Build inputs

| Input | Value |
| --- | --- |
| Tag | \`${TAG}\` |
| Commit | \`$(git rev-parse HEAD)\` |
| Commit time (UTC) | \`$(TZ=UTC git show -s --format=%cd --date=format-local:%Y-%m-%dT%H:%M:%SZ HEAD)\` |
| Go (pinned) | \`${GO_VERSION:-unset}\` |
| Go (ran) | \`$(tool go go version)\` |
| GoReleaser (pinned) | \`${GORELEASER_VERSION:-unset}\` |
| cosign (pinned) | \`${COSIGN_VERSION:-unset}\` |
| cosign (ran) | \`$(cosign version 2>&1 | grep -m1 GitVersion | tr -s ' ' || echo 'cosign not on PATH')\` |
| syft (pinned) | \`${SYFT_VERSION:-unset}\` |
| syft (ran) | \`$(tool syft syft --version)\` |
| Runner image | \`${ImageOS:-unknown} ${ImageVersion:-unknown}\` |
| Build flags | \`GOFLAGS=${GOFLAGS:-} GOPROXY=${GOPROXY:-} CGO_ENABLED=${CGO_ENABLED:-}\` |
| go.sum sha256 | \`$(sum go.sum)\` |
| vendor/modules.txt sha256 | \`$(sum vendor/modules.txt)\` |

Rebuild: check out the tag, install the pinned Go and GoReleaser, run
\`make completions\`, then \`HOMEBREW_TAP_TOKEN=unused goreleaser release --clean --skip=publish,sign\`.
Binaries, archives and packages are pinned to the commit time and are expected to match
\`checksums.txt\`; the signature never will (it is keyless), and the SBOM bundle also
depends on the syft version above.
MD
