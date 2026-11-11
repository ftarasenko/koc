# Security policy

`koc` is an OpenStack client. It reads cloud credentials from `clouds.yaml`,
`OS_*` environment variables, HashiCorp Vault and Kubernetes secrets, holds a
Keystone token for the life of an invocation, and makes authenticated TLS calls
to cloud endpoints. Credential handling and certificate verification are the
parts of it worth attacking, so they are the parts this policy is about.

## Supported versions

**Only the latest release is supported.** There are no backports and no
maintenance branches: a fix ships in the next release, and the upgrade path for
every earlier version is to move to the newest one.

| Version | Supported |
| --- | --- |
| latest release | yes |
| anything older | no — upgrade |

While the project is `0.y.z` there is no stable API, so a minor release can
change behaviour. The release notes call out breaking changes under their own
heading; read them before upgrading a pinned deployment.

## Reporting a vulnerability

Use GitHub's **private vulnerability reporting** on this repository
(Security → Report a vulnerability). It creates a private thread visible only to
the maintainers.

**Do not open a public issue** for anything touching credentials, TLS
verification, `--debug` output or the release pipeline. A public report on those
is itself an exposure, because it tells everyone running an unpatched binary
where to look.

Please include the `koc` version (`koc --version`), what you observed, and how to
reproduce it. If a reproduction needs request/response captures, **redact them
first** — `--debug` output contains tokens, passwords and endpoint addresses.

Expect an acknowledgement within a few working days. This is a small project;
there is no 24/7 rotation, and an honest slow answer is better than a promised
fast one.

## In scope

- Credential handling: `--creds-from-vault`, `--creds-from-ns`, `clouds.yaml`
  and `OS_*` parsing, and anything that causes a credential to reach a host it
  was not intended for.
- TLS: certificate verification, the `--insecure` and `OS_INSECURE` paths, CA
  bundle and mTLS handling.
- `--debug` redaction — any secret that survives into debug output.
- Secrets reaching disk or terminal output where they should not, including
  `koc vault kv export`.
- The release supply chain: the build, the SBOMs, the signature, the
  provenance attestation.

## Out of scope

- A misconfigured or vulnerable OpenStack cloud. `koc` is a client; report those
  upstream.
- Anything requiring an attacker who already controls the machine running `koc`
  or the credentials it was given.
- Missing hardening with no exploitable consequence. It may still be a welcome
  issue — just not a vulnerability report.

## Verifying a release

Every release ships a `checksums.txt` covering all artifacts, a **keyless cosign
signature** over that file, and one **SPDX SBOM per artifact**, tarred together
as `koc_<version>_sboms.tar.gz`.

`checksums.txt` on its own proves nothing — whoever could swap an artifact could
swap the checksums with it — so the signature is what roots the trust, and the
checksums chain from it to each artifact.

Verify the signature, then the artifact:

```sh
V=v0.44.0   # the release you downloaded
cosign verify-blob checksums.txt --bundle checksums.txt.bundle \
  --certificate-identity "https://github.com/ftarasenko/koc/.github/workflows/release.yml@refs/tags/$V" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

sha256sum -c checksums.txt
```

No key distribution is involved: the signing identity is the release workflow
itself, asserted by the certificate. The `--certificate-identity` and
`--certificate-oidc-issuer` flags are the check that matters — without them you
have verified only that *somebody* signed the file.

The repository was `ftarasenko/go-openstackclient` until v0.42.0, and the
certificates of those releases name it. For v0.42.0 and older, put
`go-openstackclient` in place of `koc` in the identity and in
`gh attestation verify --repo`.

#### Signing identity

The certificate that signed `checksums.txt` is issued to one workflow run, and
the identity to accept is this exact pair:

| Field | Value |
| --- | --- |
| Certificate identity (SAN) | `https://github.com/ftarasenko/koc/.github/workflows/release.yml@refs/tags/<version>`, for example `.../release.yml@refs/tags/v0.44.0` |
| OIDC issuer | `https://token.actions.githubusercontent.com` |

The identity is matched exactly, with `--certificate-identity`, not by a
pattern: it fixes the repository, the workflow file and the release's own tag. A
signature produced by another workflow in this repository (`ci.yml`, a fork's
workflow), by a branch ref or by another release's tag does not verify. To check
every release with one command, an anchored pattern is acceptable:

```sh
cosign verify-blob checksums.txt --bundle checksums.txt.bundle \
  --certificate-identity-regexp '^https://github\.com/ftarasenko/koc/\.github/workflows/release\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Both forms were checked against the v0.44.0 bundle (accepted); the exact form
with `ci.yml` in place of `release.yml`, or with another tag, was rejected. If `release.yml` is ever renamed or moved, this identity changes with
it: update this section and the README command in the same commit
(`test/release/gate_test.sh` fails otherwise).

#### What a release is built from

A release is cut only from an annotated `v*` tag whose commit is on `master` and
whose required CI check-runs (build-test, test-race, lint, the six crossbuilds
and the vendor re-derivation) all succeeded on that exact SHA;
`scripts/release-gate.sh` enforces this in the `gate` job before the job that holds
signing and publishing rights starts. The same gate runs without publishing from
`release-dryrun.yml`, which also builds the artifacts with the pinned tools.
Go, GoReleaser, cosign and syft are pinned to exact versions in `release.yml`,
and each release's notes end with a "Build inputs" table: commit, tool versions
pinned and observed, runner image, and the hashes of `go.sum` and
`vendor/modules.txt`. The gate guards against a mistaken tag; it is not a boundary
against someone who can both push a tag and edit the workflow. That boundary is
the `release` environment (its PAT is reachable only from `v*` tags) and the tag
ruleset.

`checksums.txt.bundle` is a Sigstore bundle carrying the signature, the signing
certificate and the transparency-log inclusion proof together, so it is the only
file to fetch beside `checksums.txt` itself. Reading it needs **cosign v3+**,
where that format is the default, or cosign v2.6+ with an explicit
`--new-bundle-format`. Releases up to and including **v0.22.0** shipped a
detached `checksums.txt.sig` + `checksums.txt.pem` pair instead; substitute
`--certificate checksums.txt.pem --signature checksums.txt.sig` for `--bundle`
and a v3 client verifies those too — it detects the legacy layout and falls back
on its own.

Provenance — what built the artifact, from which commit and workflow:

```sh
gh attestation verify koc_<version>_linux_amd64.tar.gz \
  --repo ftarasenko/koc
```

The SBOMs ship as one asset, `koc_<version>_sboms.tar.gz`. Unpack it to get one
SPDX 2.3 JSON document per artifact, named `<artifact>.spdx.json`, for feeding an
inventory or licence scanner:

```sh
tar xzf koc_<version>_sboms.tar.gz
```

The bundle is listed in `checksums.txt`, so it is covered by the signature above
like every other artifact. Its documents are normalised to be byte-reproducible:
syft otherwise stamps each one with a wall-clock timestamp and a random
namespace UUID, which would make the SBOM line in `checksums.txt` impossible to
re-derive from a rebuild.

### Verifying without internet access

The commands above are the reason the air-gap matters here: by default
`cosign verify-blob` fetches Sigstore's trust roots over TUF and
`gh attestation verify` calls the GitHub API, so both fail on an isolated
network.

The bundle already contains the signature, the certificate and the
transparency-log inclusion proof, so the only thing still missing inside the
enclave is the trust anchor those are checked against. Capture it once on a
connected host as a **trusted root** file, carry it across with the release
artifacts, and the verification runs with no network at all:

```sh
# On a connected host, once per trust-root rotation (not per release):
cosign trusted-root create --with-default-services --out trusted_root.json

# Inside the enclave — no Rekor call, no TUF fetch:
cosign verify-blob checksums.txt --bundle checksums.txt.bundle \
  --trusted-root trusted_root.json \
  --certificate-identity "https://github.com/ftarasenko/koc/.github/workflows/release.yml@refs/tags/$V" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Keep the identity flags on the enclave-side command. They are what bind the
signature to *this* repository's release workflow; a `--trusted-root` on its own
only proves the certificate came from Sigstore, which any GitHub workflow can
obtain.

`--trusted-root` requires the new bundle format, which is why it works from
v0.23.0 onward. For an older release, the equivalent is to run the full
verification on the connected host and carry the *result* across, since the
detached `.sig`/`.pem` pair has no inclusion proof to check offline against.

`gh attestation verify` accepts `--bundle` for the same purpose; download the
attestation bundle on the connected side and pass it in.

Once `checksums.txt` is trusted, `sha256sum -c checksums.txt` is fully offline
and is what you actually gate the install on.

### Re-deriving the binary yourself

Builds are byte-reproducible: the binary timestamp, the archive member
timestamps and the package timestamps are all pinned to the commit. Two builds
of the same tag produce identical checksums, so you can rebuild from source and
compare against `checksums.txt` rather than trusting the published bytes at all.
This also means a release whose binaries were later pruned to save storage can
still be reconstructed from its tag and checked against the retained checksums.

## Known trade-offs

These are deliberate, documented, and not vulnerabilities — but you should know
about them:

- `--insecure` / `OS_INSECURE` disables TLS certificate verification. It warns
  on stderr on every path that honours it. Do not use it against anything you
  care about.
- `--creds-from-ns` falls back to plain `http://` when the Ironic resource
  advertises no TLS certificate, and warns when it does. Basic-auth credentials
  then cross the network in cleartext.
- `koc vault kv get` prints secret values in cleartext by design; that is the
  command's purpose. `koc vault kv export` writes `0600` and encrypts to a
  recipient key.
- Passing a secret as a command-line flag exposes it to other users via the
  process table on most systems. Prefer the environment or a credential source.
