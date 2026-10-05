# Authenticated releases

A release is published only after the **same source SHA** passes the complete
Linux verification profile, both Go race suites, real PostgreSQL/NATS/OIDC and
recovery tests, mandatory browser scenarios, documentation/toolchain recipes,
the installed systemd smoke, measured capacity/developer soak and five native binary checks. Failed or missing
jobs block signing/publication. Unit fixtures are not independent integration
evidence. The source smoke proves unsigned updates are refused; the release smoke
uses the actual signed manifest to exercise the updater, backup, same-version
no-op and production configuration/journal preservation.

## Trust and verification

Install a trusted GitHub CLI with `gh attestation verify` support first
(the reference verifier is CLI 2.96.0), and Python 3 on Linux/macOS. Windows uses
PowerShell JSON and SHA256 facilities. There is no unsigned fallback.
The CLI verifies the Sigstore certificate, transparency evidence, repository
`Dr0nj/regente`, signer `.github/workflows/release.yml`, source SHA/ref and
GitHub-hosted runner. Only main or the exact version tag is accepted.
The signed `release-manifest.json` binds version, schema/protocol policy, sizes
and SHA256 digests of every payload, including the SPDX SBOM. Downloads are pinned
to that authenticated version before use. A checksum downloaded beside a payload
is not an independent trust anchor.

The **bootstrap script and verifier themselves must already be trusted**.
A downloaded script cannot establish trust in its own earlier execution.
Use a reviewed checkout at a trusted commit, or verify the manifest with your
trusted CLI and check the script's size/hash from that manifest before executing
it. Do not treat `curl | sudo bash` or `irm | iex` as authenticated bootstrap.
For a pinned release, download its manifest/signature and the installer without
executing them, inspect the version/ref/SHA, then:

```sh
set -e
gh attestation verify release-manifest.json --bundle release-manifest.sigstore.json \
  --repo Dr0nj/regente --signer-workflow Dr0nj/regente/.github/workflows/release.yml \
  --source-digest "$REVIEWED_SHA" --source-ref "$REVIEWED_REF" --deny-self-hosted-runners
python3 - <<'PY'
import hashlib,json
from pathlib import Path
m=json.load(open("release-manifest.json"))
p=Path("install.sh");a=m["assets"][p.name]
if p.stat().st_size!=a["bytes"] or hashlib.sha256(p.read_bytes()).hexdigest()!=a["sha256"]:
    raise SystemExit("Bootstrap integrity failed")
PY
sudo bash install.sh
```

Do not execute the last command if either check fails. Equivalent native
SHA256/JSON checks apply to the Windows bootstrap. Repository override is refused.
The release workflow is part of the publisher trust boundary; its compromise or
a compromised trusted CLI/host is not repaired by artifact signing.

For an offline Linux update provide all three local inputs:

```sh
sudo REGENTE_BUNDLE=/trusted/bundle.tar.gz \
  REGENTE_MANIFEST=/trusted/release-manifest.json \
  REGENTE_ATTESTATION=/trusted/release-manifest.sigstore.json regente-update vX.Y.Z
```

The same identity and bytes checks run before extraction or execution. Sigstore
trust roots must be available to the trusted CLI (pre-populate its cache for a
fully disconnected deployment). An old unsigned release is refused, even with
`--force`. Force only reinstalls an authenticated version.

## Platform and compatibility scope

| Platform | Release evidence |
|---|---|
| Linux amd64 | Actual bundle install, reboot, drained v0.2.47-to-current update and authenticated same-build update, DB backup/no-op, credentials/journal preservation, execution security and independent audit collector |
| Linux arm64 | Native version output and fresh/repeated SQLite migration |
| Windows amd64 | Actual Windows installer verifier/signature and tamper refusal, native server/agent version output and fresh/repeated SQLite migration; existing native demo recipe |
| macOS amd64 / arm64 | Native server/agent version output and fresh/repeated SQLite migration |

Native binary checks do not certify every platform's service installer, fleet
upgrade or workload. Server and agent must use the same tested release and
protocol 2 in production; schema 32 accepts [32,32]. **No N/N−1 rolling pair is
qualified.** Schema refusal/legacy migration/recovery tests remain mandatory.
Follow [upgrades](upgrades.md) for all-node drain and complete recovery sets;
a retained old binary does not downgrade a migrated database.

## Repository enforcement

Inspection on 2026-10-03 found main **unprotected**, no repository rulesets,
administrator access and automatic merging disabled. These facts are not a
branch-protection claim. The publication DAG enforces the release gates independently.
Publication refuses an existing release version; replacement requires a new version.
Direct changes to publisher workflows still require review and trusted maintainers.
Actions used by these workflows are pinned to reviewed commit SHAs; Syft is pinned
to v1.54.0. The SBOM inventories actual Go artifacts and the frontend lockfile;
it is an inventory, not a vulnerability clearance.

Primary verifier references:
[GitHub attestations](https://github.com/actions/attest),
[CLI verification policy](https://cli.github.com/manual/gh_attestation_verify).

Windows offline verification accepts REGENTE_MANIFEST, REGENTE_ATTESTATION and
REGENTE_RELEASE_ASSET (the exact executable asset). It applies the same identity
and size/hash policy before replacing the installed agent.
