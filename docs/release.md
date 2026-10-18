# Releasing queryshift

A release is a tag. `git tag vX.Y.Z && git push origin vX.Y.Z` runs
`.github/workflows/release.yml`: GoReleaser builds the six platform binaries,
signs the checksums with cosign (keyless), publishes the GitHub Release + SBOMs +
.deb/.rpm + the JSON Schemas, pushes the multi-arch image to ghcr.io, publishes
the npm packages, and pushes the Homebrew formula. Post-release smoke jobs then
install from each channel as a stranger would (`npx`, `brew`, `docker run`,
`cosign verify-blob`) and fail the run if a channel didn't actually ship.

Before tagging: `CHANGELOG.md` has the section, `scripts/gate.sh` is green, and
`gh run list --branch main` shows CI green on the commit you're tagging.

After the release is green, move the floating GitHub Action major tag so
`uses: sarankumar1325/queryshift@v1` picks up the new root `action.yml`
(`release.yml` only fires on full `vX.Y.Z` tags, so this never cuts a release):

```sh
git tag -f v1 vX.Y.Z && git push -f origin v1
```

## Channels and the secret each one needs

| Channel | Needs | If missing |
|---|---|---|
| GitHub Release, cosign, ghcr.io, .deb/.rpm, schemas | nothing (workflow `GITHUB_TOKEN` + OIDC) | — |
| npm (`@queryshift/cli` + `@queryshift/<os>-<arch>`) | `NPM_TOKEN` (a **Classic Automation** token — bypasses 2FA by design) | publish step skips; `npm-smoke` jobs are skipped |
| Homebrew (`brew install sarankumar1325/tap/queryshift`) | `HOMEBREW_TAP_DEPLOY_KEY` (see below) | formula is generated but **not pushed**; `brew-smoke` **fails** so the gap is visible |

## Homebrew: how the tap is wired

`brew install sarankumar1325/tap/queryshift` resolves to the repository
[`sarankumar1325/homebrew-tap`](https://github.com/sarankumar1325/homebrew-tap), which
holds `Formula/queryshift.rb`. GoReleaser's `brews` block (`.goreleaser.yaml`)
regenerates that file on every tag — download URLs and SHA-256s for all four
macOS/Linux archives — and pushes it to the tap. The workflow's `GITHUB_TOKEN`
cannot write to another repository, so the push needs **one** of two
credentials, read from `release.yml` secrets (first one found wins):

| Secret on `sarankumar1325/queryshift` | Mechanism | Notes |
|---|---|---|
| `HOMEBREW_TAP_DEPLOY_KEY` | git+SSH push with a write **deploy key** registered on the tap repo | Preferred: scoped to exactly one repo, no expiry, and not tied to a person. |
| `HOMEBREW_TAP_TOKEN` | GitHub Contents API with a **fine-grained PAT** | Resource owner `sarankumar1325`, repository access **only `homebrew-tap`**, permission **Contents: read and write**. Tied to the creating account and it expires (≤ 1 year) — when it lapses the push skips and `brew-smoke` fails the release, so it's visible. |

### Option A — deploy key (one-time; repeat only to rotate)

```bash
# 1. A fresh ed25519 key pair, no passphrase (GoReleaser can't prompt).
ssh-keygen -t ed25519 -N "" -C "queryshift release → homebrew-tap" -f /tmp/queryshift-tap-key

# 2. Public half → write-enabled deploy key on the TAP repo.
gh repo deploy-key add /tmp/queryshift-tap-key.pub --repo sarankumar1325/homebrew-tap \
  --title "queryshift release workflow (goreleaser brews)" --allow-write

# 3. Private half → secret on the QUERYSHIFT repo, read by release.yml.
gh secret set HOMEBREW_TAP_DEPLOY_KEY --repo sarankumar1325/queryshift < /tmp/queryshift-tap-key

# 4. Don't leave the private key on disk.
rm -f /tmp/queryshift-tap-key /tmp/queryshift-tap-key.pub
```

### Option B — fine-grained PAT

1. <https://github.com/settings/personal-access-tokens/new> → Resource owner
   **sarankumar1325** → Repository access: *Only select repositories* →
   `homebrew-tap` → Permissions → Repository → **Contents: Read and write**.
2. `gh secret set HOMEBREW_TAP_TOKEN --repo sarankumar1325/queryshift` (paste the token).

**Known follow-up.** GoReleaser (v2.16+) marks the `brews` (formula) section
deprecated in favour of `homebrew_casks`; it still works on every v2.x and the
workflow pins `~> v2`, so nothing breaks — but it will be removed in a future
major. The formula is kept on purpose for now: Homebrew does **not** quarantine
formula downloads, so the unsigned/un-notarized Go binary runs as installed on
macOS, whereas a cask needs either Apple notarization or the `xattr -dr
com.apple.quarantine` post-install hook from the GoReleaser docs. Revisit when
moving to GoReleaser v3 (casks now support Linux binaries too).

The formula is deliberately **generated, not hand-maintained**: if you ever need
to repair it by hand (e.g. a release ran before the key existed), copy
`dist/homebrew/queryshift.rb` from a local `goreleaser release --snapshot --skip=publish`
— or write the same shape from the release's `checksums.txt` — and push it to the
tap; the next tag overwrites it anyway.

## After the run

`gh run watch` until every job is green, then spot-check one channel you didn't
change. A red `brew-smoke` / `npm-smoke` with a green `goreleaser` job means the
binaries shipped but a channel didn't — fix the secret and re-tag
(`vX.Y.Z+1`; tags are immutable).
