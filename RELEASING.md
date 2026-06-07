# Releasing nt

Releases are automated by GitHub Actions + [GoReleaser](https://goreleaser.com).
Pushing a semver tag builds cross-platform binaries and publishes a GitHub
Release; the curl installer and `go install` pick it up from there.

## Cut a release

```sh
git tag v0.1.0
git push origin v0.1.0
```

The `Release` workflow then builds `linux`/`darwin` × `amd64`/`arm64` binaries +
`checksums.txt` and creates the GitHub Release with a changelog. Users install
with:

```sh
curl -fsSL https://raw.githubusercontent.com/navbytes/nt/main/install.sh | bash
# or
go install github.com/navbytes/nt@latest
```

## Local dry run

No tag, no publish — just verify the build:

```sh
make snapshot        # goreleaser release --snapshot --clean
goreleaser check     # validate .goreleaser.yaml
```

## Enabling Homebrew later (optional)

Deferred for now. To add `brew install navbytes/tap/nt`:

1. Create an empty public repo `navbytes/homebrew-tap`.
2. Create a fine-grained PAT with **Contents: read/write** on that tap repo and
   add it to *this* repo as the secret **`HOMEBREW_TAP_GITHUB_TOKEN`** (the
   default `GITHUB_TOKEN` can't push to a different repo).
3. Uncomment the `brews:` block in `.goreleaser.yaml` and the
   `HOMEBREW_TAP_GITHUB_TOKEN` line in `.github/workflows/release.yml`.

The next tag will then also commit `Formula/nt.rb` into the tap.
