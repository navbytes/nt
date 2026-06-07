# Releasing the desktop app

The `desktop` job in [`.github/workflows/release.yml`](../.github/workflows/release.yml)
builds native bundles with `wails build` on a macOS/Linux/Windows matrix and
uploads them to the GitHub Release that GoReleaser creates for the same tag.
It runs `needs: goreleaser`, so a desktop failure never unpublishes the CLI
release.

Per tag it attaches:

| File | Platform |
|---|---|
| `nt_<ver>_macos_universal.zip` | macOS (Apple Silicon + Intel, universal) |
| `nt_<ver>_linux_amd64.tar.gz` | Linux x86-64 (needs `libgtk-3` + `libwebkit2gtk-4.1` at runtime) |
| `nt_<ver>_windows_amd64.zip` | Windows x86-64 (uses the Edge WebView2 runtime) |

## macOS signing + notarization

**Without the secrets below**, the macOS app is uploaded *ad-hoc-signed*: it
runs, but users must right-click → **Open** once to clear Gatekeeper. Add the
secrets and tags after this are signed with your Developer ID and notarized, so
they open with a normal double-click.

Create these six **repository secrets**
(Settings → Secrets and variables → Actions → New repository secret):

| Secret | What it is |
|---|---|
| `APPLE_DEVELOPER_ID` | The signing identity, e.g. `Developer ID Application: Your Name (ABCDE12345)` |
| `APPLE_CERT_P12` | Base64 of your exported *Developer ID Application* certificate (`.p12`) |
| `APPLE_CERT_PASSWORD` | The password you set when exporting the `.p12` |
| `APPLE_ID` | Your Apple ID email |
| `APPLE_TEAM_ID` | Your 10-character Team ID (e.g. `ABCDE12345`) |
| `APPLE_NOTARY_PASSWORD` | An app-specific password for notarization |

### How to produce each

**1. The certificate (`APPLE_CERT_P12`, `APPLE_CERT_PASSWORD`, `APPLE_DEVELOPER_ID`)**

- In Xcode → Settings → Accounts → your team → **Manage Certificates** → `+` →
  **Developer ID Application** (or create it at
  developer.apple.com → Certificates).
- In **Keychain Access**, find that certificate (it expands to show its private
  key), right-click → **Export** → save as `nt-developer-id.p12`, set a password.
- Encode it for the secret:
  ```bash
  base64 -i nt-developer-id.p12 | pbcopy   # paste as APPLE_CERT_P12
  ```
- Get the exact identity string for `APPLE_DEVELOPER_ID`:
  ```bash
  security find-identity -v -p codesigning  # copy the "Developer ID Application: …" line
  ```

**2. Team ID (`APPLE_TEAM_ID`)** — developer.apple.com → Membership → Team ID
(also the parenthesized code in the identity string above).

**3. App-specific password (`APPLE_NOTARY_PASSWORD`)** —
appleid.apple.com → Sign-In and Security → **App-Specific Passwords** → generate
one (e.g. named "nt notarytool"). Use that value, **not** your Apple ID password.

### Bundle identifier

The app ships as `com.navbytes.nt` (set in
[`build/darwin/Info.plist`](build/darwin/Info.plist)). Change it there if you
sign under a different identity/namespace.

## Local dry run

```bash
cd desktop
wails build -tags production -platform darwin/universal -s   # → build/bin/nt.app (ad-hoc)
# sign exactly as CI does (needs your Developer ID in the local keychain):
codesign --force --options runtime --timestamp \
  --entitlements build/darwin/entitlements.plist \
  --sign "Developer ID Application: Your Name (ABCDE12345)" build/bin/nt.app
codesign --verify --strict --verbose=2 build/bin/nt.app
```

## Linux / Windows

No signing is configured. Linux builds with the `webkit2_41` tag against
WebKitGTK 4.1; the CI installs `libgtk-3-dev` + `libwebkit2gtk-4.1-dev`. Windows
relies on the system Edge WebView2 runtime (present on current Windows).
