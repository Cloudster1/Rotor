# Releasing

Rotor releases the way Exciter, Armature and Stator do: a version is cut by
`./release.sh`, reviewed as a pull request, and published from the tag that
merging it sets. Nothing is built on the releasing machine.

## Cutting a release

From an up-to-date `main` with a clean working tree:

```sh
./release.sh --dry-run      # the version, the rule that chose it, the notes
./release.sh                # or ./release.sh 1.4.0 to choose the version
```

1. `release.sh` works out the next version from the Conventional Commits
   since the last tag and prints the rule it applied: a breaking change is
   MAJOR (MINOR below 1.0.0), a `feat` MINOR, anything else PATCH. Before the
   first tag, the version in `VERSION` is released as it is.
2. It refuses a checkout that is not on `main` or is behind `origin/main`,
   a working tree with uncommitted changes, and a version whose tag or
   release branch already exists.
3. On a new branch `release/vX.Y.Z` it writes the version into every file
   that carries one (`scripts/version-files.sh files`: `VERSION`, the
   `version` and `appVersion` of `deploy/charts/rotor/Chart.yaml`, the
   runner's `runner/pyproject.toml` and its entry in `runner/uv.lock`, and a
   `package.json` once one carries a version). It turns the `Unreleased`
   section of `CHANGELOG.md` (or, when it is empty, the commit subjects) into
   the section `## [X.Y.Z]`, commits `release: vX.Y.Z`, pushes the branch and
   opens its pull request with `gh`.
4. CI runs the whole gate on that pull request, including the test that
   refuses the version files disagreeing. Merge it with a merge commit.
5. `.github/workflows/release.yml` sees the merged `release/vX.Y.Z`, and
   `scripts/release-tag.sh` sets the annotated tag `vX.Y.Z` on the merge
   commit, with the release's changelog section as its message. It refuses a
   commit `main` does not have, version files that say another version, and
   a tag that already points elsewhere.
6. In the same run, from the tag:
   - the images `backend`, `web` and `runner` are built for amd64 with
     `VERSION` and `COMMIT` and pushed to `ghcr.io/cloudster1/rotor-<image>`
     as `X.Y.Z` and as the short commit;
   - each image is signed keyless by the workflow's own identity, its SPDX
     SBOM is attached as a signed attestation, and both are verified as a
     cluster would (`make sbom sign attest verify COSIGN_KEYLESS=1`);
   - the chart is packaged, pushed to `oci://ghcr.io/cloudster1/charts/rotor`
     as version `X.Y.Z` and signed the same way;
   - the GitHub release `vX.Y.Z` is published with the tag message as its
     notes, followed by every image's and the chart's digest, with the chart
     and the SBOMs attached;
   - `praetorianer777/Dynamo` gets a `repository_dispatch` of type
     `tool-released` with `{"tool": "rotor", "version": "X.Y.Z"}`.
7. Back on `main`: `git switch main && git merge --ff-only origin/main`.

A build that is not made from a tag says so in its version, `X.Y.Z+N.g<hash>`
or `X.Y.Z+dev` (`scripts/build-stamp.sh`), so a development image is never
taken for a release.

## Installing a release

```sh
helm install rotor oci://ghcr.io/cloudster1/charts/rotor --version X.Y.Z -f my-values.yaml
```

The chart's images default to `ghcr.io/cloudster1/rotor-<image>` at the
chart's `appVersion`. Name them by the digests in the release notes instead
(`images.<image>.digest`), so the cluster runs exactly what was signed.
`docs/deployment.md` describes the settings and how to install.

## Checking a release

Every image and the chart are signed by
`https://github.com/Cloudster1/Rotor/.github/workflows/release.yml` at the
release's tag, with a certificate from sigstore's public Fulcio that the
public transparency log records:

```sh
make verify IMAGES=ghcr.io/cloudster1/rotor-runner@sha256:... COSIGN_KEYLESS=1
cosign verify ghcr.io/cloudster1/charts/rotor@sha256:... \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/Cloudster1/Rotor/.github/workflows/release.yml@refs/tags/v'
```

`make verify` checks the signature and the SBOM attestation against that
identity. The chart's sandbox admission trusts the same identity when no key
is given (`sandbox.admission.keyless`, `docs/deployment.md`).

## When something fails

Every step of `release.yml` can run again for the same version. Re-run the
failed jobs, or, for a tag whose run is gone, run the workflow by hand:
`gh workflow run release.yml -f tag=vX.Y.Z`. A tag pushed by hand onto a
commit on `main` publishes the same way. A published version is never moved
or released again; fix forward with the next version.

## Setup

- **`DYNAMO_DISPATCH_TOKEN`**, a repository secret: a fine-grained token with
  Contents read and write on `praetorianer777/Dynamo`, which is what a
  `repository_dispatch` needs. Without it the release still publishes and the
  run says Dynamo was not told.
- **Package visibility.** GitHub creates the `rotor-*` image packages and the
  `charts/rotor` package on the first push, private. Make them public
  (Package settings, Change visibility) for clusters and policy-controller to
  pull and check them without a pull secret, or give the cluster one through
  `images.pullSecrets`.
- **Tags.** The workflow's own token sets the tag and pushes the packages; it
  asks for `contents: write`, `packages: write` and `id-token: write` in the
  jobs that need them. A ruleset that restricts creating `v*` tags has to let
  GitHub Actions through.
