# Releasing nt

Releases are automated by GitHub Actions + [GoReleaser](https://goreleaser.com).
Pushing a semver tag builds cross-platform binaries, publishes a GitHub Release,
and updates the Homebrew tap.

## One-time setup

1. **Create the tap repo.** Make an empty public repo `navbytes/homebrew-tap`
   (GoReleaser commits the formula into it as `Formula/nt.rb`).

2. **Add a tap token.** The default `GITHUB_TOKEN` can't push to a *different*
   repo, so create a fine-grained PAT with **Contents: read/write** on
   `navbytes/homebrew-tap`, and add it to *this* repo as the secret
   **`HOMEBREW_TAP_GITHUB_TOKEN`** (Settings → Secrets and variables → Actions).

## Cut a release

```sh
git tag v0.1.0
git push origin v0.1.0
```

The `Release` workflow then:
- builds `linux`/`darwin` × `amd64`/`arm64` binaries + `checksums.txt`,
- creates the GitHub Release with a changelog, and
- commits/updates `Formula/nt.rb` in `navbytes/homebrew-tap`.

After that, users can:

```sh
brew install navbytes/tap/nt
```

## Local dry run

No tag, no publish — just verify the build:

```sh
make snapshot        # goreleaser release --snapshot --clean
# or validate the config:
goreleaser check
```
