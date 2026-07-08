# Releasing nt

Releases are automated by GitHub Actions + [GoReleaser](https://goreleaser.com).
Pushing a semver tag builds cross-platform binaries and publishes a GitHub
Release; the curl installer and `go install` pick it up from there.

## Cut a release

```sh
git tag v0.1.0
git push origin v0.1.0
```

The `Release` workflow then builds `linux`/`darwin`/`windows` × `amd64`/`arm64`
binaries + `checksums.txt` and creates the GitHub Release with a changelog. Users install
with:

```sh
curl -fsSL https://raw.githubusercontent.com/navbytes/nt/main/install.sh | bash
# or
go install github.com/navbytes/nt@latest
# or
brew install navbytes/tap/nt
# or (not yet in the official mise registry — installs straight from GitHub releases)
mise use -g github:navbytes/nt
```

Each tag also commits an updated `Casks/nt.rb` to `navbytes/homebrew-tap`, via
the `HOMEBREW_TAP_GITHUB_TOKEN` secret (a fine-grained PAT scoped to that repo's
Contents: read/write — the default `GITHUB_TOKEN` can't push to a different repo).

## Local dry run

No tag, no publish — just verify the build:

```sh
make snapshot        # goreleaser release --snapshot --clean
goreleaser check     # validate .goreleaser.yaml
```
