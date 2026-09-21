# Changes and releases

All code changes go through pull requests. Draft PRs skip expensive validation;
mark the PR ready to run relevant checks on the current revision. Superseded PR
runs cancel. Do not bypass required checks.

## Prepare a release

Update release notes and applicable version examples, and write the tag (for example `v1.2.3`) to `.github/release-request`. Its merge publishes CLI and desktop assets from that exact commit.

Use a local authenticated `gh` session, directly or through an LLM, to create a
branch named `release/<version>`, make the version and release-note changes, and open a draft PR with
`gh pr create --draft`. Mark it ready with `gh pr ready` when preparation is
complete. Wait for required checks before merging.

"Prepare a release" stops at the PR. A human merge, or an explicit instruction
to an LLM to release, authorizes merging that PR and publishing. Never merge a
release or dependency PR unattended. Never create a new version merely to retry
a failed publisher: inspect the failed run and its published artifacts first.

The local gh path needs no new secrets. A future Prepare release Action must
have permission to create PRs; token-created PRs need a human ready event (or
a separately authorized token) to trigger normal PR CI. No such token is assumed.

Routine dependency updates share one weekly multi-ecosystem group. Security updates
remain eligible immediately and are not held for the routine weekly batch.

Tap publishing tokens need both Contents and Pull requests write permission on
the destination tap. Tap updates require review and merge after binary publication.
