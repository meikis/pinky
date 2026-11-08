# Releasing Pinky

Pinky releases often, several times a day when there is something to ship, and nobody builds a release by hand: the agent that pushes to `main` writes the notes and runs one command, and a workflow does the rest, in the open. The rule for when a push is a release is in [CLAUDE.md](../CLAUDE.md).

## How a release happens

1. The notes wait in [CHANGELOG.md](../CHANGELOG.md) under `## Unreleased`.
2. `scripts/release.sh tag -push` gives them the next version, commits `chore(release): <version>`, tags it `v<version>` and pushes `main` and the tag together (`git push --atomic`).
3. The tag starts [.github/workflows/release.yml](../.github/workflows/release.yml), which runs the scripts as they are at the tag, on GitHub's runners (free, macOS's too, as the repository is public):
   - **Companion APK** (Ubuntu): checks the tag and its notes, then `scripts/dist.sh companion`, which builds the companion's release build signed with its release key ([below](#the-companions-signing-key)).
   - **Linux and Windows** (Ubuntu): `scripts/dist.sh windows`, `scripts/dist.sh linux`, with their FFmpeg and adb (below), then decodes test streams with the FFmpeg that the `.tar.gz` carries and starts a server with its adb.
   - **Windows package** (GitHub's Windows runner): decodes test streams with the FFmpeg that the Windows zip carries and starts a server with its adb.
   - **macOS**: `scripts/dist.sh darwin` builds Pinky.app for Apple silicon and Intel, each with its own FFmpeg and adb, signs them with the Developer ID, notarizes both disk images at once and staples the tickets to the disk images and the apps, then starts a server with each app's adb (the Intel one's where the runner has Rosetta).
   - **Publish** (Ubuntu), once the others passed: `release manifest` writes `latest.json`, the Homebrew cask `pinky.rb` and `SHA256SUMS`, and `scripts/publish.sh` creates the GitHub release as a draft, uploads everything (`latest.json` last) and publishes it.
4. Within a quarter of an hour, the workflow of the tap, [DIYgod/homebrew-tap](https://github.com/DIYgod/homebrew-tap), takes `pinky.rb` from the latest release into `Casks/pinky.rb`, so this repository needs no token for the tap.

On this Mac the whole build, notarization included, takes about two minutes once FFmpeg and adb are built. On GitHub the first run (7 November 2026, without publishing) took 12 minutes: 5 of them went to building FFmpeg for both Macs, and Apple's notary service answered within a minute. Pinky's own adb adds about 13 minutes to the Linux and Windows job (7 for Windows, 6 for Linux) and 5 to the macOS one when the cache has no build of it (8 November 2026). A run for a tag finds no cache that another tag's run saved (GitHub keeps a tag's caches to that tag), so a release builds both again: expect about 25 minutes from the push to the published release.

## Versions

A version is `YEAR.MDD.N`: the release's date in Asia/Shanghai (the month without a leading zero, then the two-digit day) and that day's count from 1. `2026.1107.1`, `2026.1107.2`, `2027.105.1`. Every part is an integer without leading zeros, so versions are valid semver, compare part by part, and fit macOS bundle versions and Windows version resources. Tags are `v<version>`. `scripts/release.sh next` prints the next one, counting the tags here and on `origin`.

The version is not kept in the source. A release writes it into its checkout's `cmd/pinky/mygo.json` (`go run ./scripts/release stamp <version>`) before building, which gives it to the app (`Info.plist`, the Windows version resource, the packages), to `pinky version` (`-X main.version`), and to the companion APK's versionName. Builds from the source (`make app`, `go build`) say `0.0.0-dev`, which is not a release version, so they never look for updates.

Go modules read a `v2026…` tag as major version 2026, which the module path does not carry, so `go install github.com/DIYgod/pinky/cmd/pinky@latest` takes the newest commit rather than a release.

## What a release publishes

| File | For |
| --- | --- |
| `pinky-<version>-darwin-arm64.zip`, `pinky-<version>-darwin-amd64.zip` | The updater and the Homebrew cask: `Pinky.app`, signed, notarized and stapled, zipped with `ditto -c -k --keepParent` |
| `pinky-<version>-arm64.dmg`, `pinky-<version>-amd64.dmg` | People on macOS: the disk image, signed, notarized and stapled |
| `pinky-setup-<version>-amd64.exe` | People on Windows, and the updater, which runs it quietly (`/S /D=<the install folder>`) to update a Pinky it installed: the installer, which installs over the installed version in `%LOCALAPPDATA%\Programs\Pinky` (not signed, so SmartScreen warns when people download it) |
| `pinky-<version>-windows-amd64.zip` | People who run Pinky without installing it: the app's files (`Pinky.exe`, FFmpeg's DLLs, adb and the companion APK) |
| `pinky-<version>-linux-amd64.tar.gz` | The updater on Linux and `install.sh` of `mygo build`: `pinky`, FFmpeg's libraries, adb, its desktop entry and icon, the companion APK |
| `pinky_<version>_amd64.deb` | People on Debian and Ubuntu: the same in `/opt/pinky` |
| `pinky-<version>.mcpb` | People with Claude Desktop: an [MCP bundle](https://github.com/modelcontextprotocol/mcpb) that a double-click installs as the extension Pinky, which starts the Pinky installed on the computer (`go run ./scripts/release mcpb`, see [mcp.md](mcp.md#claude-desktop-extension)) |
| `latest.json` | The updater: the release's manifest, below |
| `pinky.rb` | The Homebrew tap: the cask of this release |
| `SHA256SUMS` | Everyone: every other file's SHA-256, as `sha256sum` writes it |

### FFmpeg

Every package carries Pinky's own FFmpeg, which `scripts/ffmpeg.sh` builds from the FFmpeg 9 source (checked against its SHA-256): a minimal LGPL build of libavutil, libavcodec (the H.264 and HEVC decoders), libswscale and libavformat, with its license and build notes (`FFmpeg-COPYING.LGPLv2.1`, `FFmpeg-BUILD.txt`).

| Package | Where | Decoding |
| --- | --- | --- |
| Pinky.app | `Contents/Frameworks`, signed with the app | VideoToolbox, or software |
| Windows installer and zip | next to `Pinky.exe` (`avcodec-63.dll` and the rest) | D3D11VA, or software |
| Linux `.tar.gz` and `.deb` | next to `pinky` (`libavcodec.so.63` and the rest, which find one another through `$ORIGIN`) | software; needs glibc 2.29 or later (Ubuntu 20.04, Debian 11) |

Pinky looks there before the system's libraries (after `PINKY_FFMPEG_DIR`), so a person can point `PINKY_FFMPEG_DIR` at another FFmpeg 9, such as one with VA-API on Linux. The Windows and Linux builds are made in a Docker container, Ubuntu 24.04's mingw-w64 for Windows and Ubuntu 20.04 for Linux (an old glibc, so that they need no newer one; FFmpeg 9's assembly needs a newer nasm than its own, which the build makes from source), the same on this Mac and on the runners, and `scripts/ffmpeg.sh` checks that they need nothing but the system's own libraries. The builds are kept by the hash of `scripts/ffmpeg.sh`: the release workflow caches them, and this Mac keeps them in `~/Library/Caches/pinky-release`. `PINKY_FFMPEG_APP=<a Pinky.app, or the folder of an unpacked Windows or Linux package> go test -run TestPackagedFFmpeg ./internal/video/` decodes with a package's FFmpeg, as the release workflow does before it publishes.

### adb

Every package carries Pinky's own adb, so that Android phones need no platform tools: `scripts/adb.sh` builds it from the source of [android-tools](https://github.com/nmeum/android-tools) 37.0.0p1 (AOSP's `android-17.0.0_r1`, Google's platform-tools 37.0.0), adb alone (`scripts/adb/CMakeLists.txt`), linked statically against protobuf 3.21.12, BoringSSL, libusb, fmt, brotli, lz4, zstd and zlib, every source checked against its SHA-256. It goes with its licenses and build notes (`adb-NOTICE.txt`, `adb-BUILD.txt`; libusb's LGPL 2.1 among them).

| Package | Where | Built |
| --- | --- | --- |
| Pinky.app | `Contents/Helpers/adb`, signed with the app | on the Mac, with CMake (Apple's clang, macOS 12 and later), for each processor |
| Windows installer and zip | `adb.exe`, `AdbWinApi.dll`, `AdbWinUsbApi.dll` next to `Pinky.exe` | a 32-bit adb.exe, as Google's, with Ubuntu 24.04's mingw-w64 in Docker; the two DLLs are AOSP's own (`platform/development`, `host/windows/prebuilt/usb`), which AOSP builds for 32-bit Windows only |
| Linux `.tar.gz` and `.deb` | `adb` next to `pinky`, executable | a static musl executable, in an Alpine container: no glibc needed |

Pinky prefers an adb the computer has (the Android SDK's, Homebrew's, PATH's) and runs its own from a copy in the user's cache folder, never from the app's folder: see [Pinky's own adb](notes/android.md#pinkys-own-adb). The builds are kept by the hash of `scripts/adb.sh` and `scripts/adb/`: the release workflow caches them, and this Mac keeps them in `~/Library/Caches/pinky-release/adb` (`ADB_CACHE`). `PINKY_ADB_APP=<a Pinky.app, or the folder of an unpacked Windows or Linux package> go test -run TestPackagedADB ./internal/driver/android/adb/` starts a server with a package's adb on a port of its own, as the release workflow does before it publishes. `make adb` builds this Mac's into `build/adb`, which `make app` puts in the app.

### The companion's signing key

Every package carries Pinky Companion's release build, `app.pinky.companion`, signed with Pinky's private release key. Android installs an update of an app only when it is signed with the installed app's key, and an update keeps the accessibility service that was on: with the key private, nobody else can ship an update of a released Companion. The key lives only in the secrets `COMPANION_KEYSTORE` and `COMPANION_KEYSTORE_PASSWORD` (and the owner's backup, below); the release workflow decodes the keystore into a temporary file for the build and deletes it afterwards, and prints the certificate the APK is signed with.

Every release must be signed with this key. An APK signed with another one cannot update installed Companions; Pinky then replaces them by itself (it compares the installed build's signer, which `dumpsys package` shows, with the bundled APK's, and uninstalls and installs it while the phone is free), but phones where Google Play Protect checks USB installs wait for the person's Update, and each replacement shows the person a notice.

The key's certificate has the SHA-256 `7A:0F:AA:EF:34:62:6E:B2:9B:88:99:55:8E:A2:FE:49:13:40:12:59:1F:43:8D:6C:B9:4B:6B:9F:16:D6:19:EF`, and `dumpsys package app.pinky.companion` names its signer `a9ed84e0`; the workflow's log shows the certificate of each release's APK.

Every build until 8 November 2026 was signed with a key committed in this repository (`android/companion/pinky-companion-debug.keystore`, public since the repository is), which Pinky replaces the same way. Builds from the source are the development build, `app.pinky.companion.dev`, signed with the machine's own debug key: they install beside a released Companion and never over it. A dry run (`scripts/release.sh build`) without the key carries one, with a warning; the workflow and `publish` stop without the key, and `scripts/dist.sh` refuses to package the development build when `STRICT` is set.

### latest.json

```json
{
  "version": "2026.1107.1",
  "tag": "v2026.1107.1",
  "date": "2026-11-07T08:09:10Z",
  "notes": "- The version's notes from CHANGELOG.md, in Markdown.",
  "assets": {
    "darwin-amd64": { "url": "https://github.com/DIYgod/pinky/releases/download/v2026.1107.1/pinky-2026.1107.1-darwin-amd64.zip", "sha256": "…", "size": 17931709 },
    "darwin-arm64": { "url": "…/pinky-2026.1107.1-darwin-arm64.zip", "sha256": "…", "size": 16508868 },
    "linux-amd64": { "url": "…/pinky-2026.1107.1-linux-amd64.tar.gz", "sha256": "…", "size": 14804370 },
    "windows-amd64": { "url": "…/pinky-setup-2026.1107.1-amd64.exe", "sha256": "…", "size": 14000000 }
  }
}
```

`https://github.com/DIYgod/pinky/releases/latest/download/latest.json` is always the newest release's: a release becomes the latest only when no newer one is out, and `latest.json` is uploaded after the files it names, before the release is published. `date` is when the release was assembled, in UTC. `notes` is the version's section of CHANGELOG.md without its heading, which `go run ./scripts/release notes <version>` prints.

### CHANGELOG.md

Newest first. Each release is a `## <version> (<YYYY-MM-DD>)` heading followed by short bullets for people; notes not released yet wait under `## Unreleased` at the top, which a dev build of the app should leave out. A heading's first word is its version (as MyGo's `update.ReleaseNotes` reads it too), and headings inside fenced code blocks do not count.

### The command

The command that the Homebrew cask (`/opt/homebrew/bin/pinkyctl`) and the Linux packages (`/usr/bin/pinkyctl` from the `.deb`, `~/.local/bin/pinkyctl` from `install.sh`) install is `pinkyctl`, not `pinky`: coreutils has a `pinky` command, which every Linux and Homebrew's coreutils install, so a `pinky` of Pinky's could not be installed there (dpkg refuses to overwrite coreutils' file) or lost to it on the `PATH`.

## Setting it up

Done once.

1. **The tap.** `CREATE=1 scripts/release.sh setup` creates [DIYgod/homebrew-tap](https://github.com/DIYgod/homebrew-tap) (public) when it does not exist, copies in its workflow and README from `scripts/release/homebrew-tap/`, and lists the secrets the release workflow lacks. Run `scripts/release.sh setup` again whenever `scripts/release/homebrew-tap/` changes.
2. **The Developer ID certificate, `MACOS_CERTIFICATE` and `MACOS_CERTIFICATE_PASSWORD`.** In Keychain Access, under My Certificates in the login keychain, right-click "Developer ID Application: NATURAL SELECTION LABS PTE. LTD. (492J8Q67PF)", choose Export, and save it as a `.p12` with a new password. Then (gh asks for the password, so it never lands in a file):

   ```sh
   base64 -i ~/Desktop/developer-id.p12 | gh secret set MACOS_CERTIFICATE --repo DIYgod/pinky
   gh secret set MACOS_CERTIFICATE_PASSWORD --repo DIYgod/pinky
   rm ~/Desktop/developer-id.p12
   ```

3. **An App Store Connect API key for notarization, `NOTARY_API_KEY`, `NOTARY_API_KEY_ID` and `NOTARY_API_ISSUER`.** At <https://appstoreconnect.apple.com/access/integrations/api> (Users and Access › Integrations › App Store Connect API › Team Keys; it takes the Admin or Account Holder role), add a key with the Developer role, download its `AuthKey_<id>.p8` (Apple offers it once), and note its Key ID and the Issuer ID above the list. A key that other workflows of the team already use works too.

   ```sh
   gh secret set NOTARY_API_KEY --repo DIYgod/pinky < ~/Downloads/AuthKey_<id>.p8
   gh secret set NOTARY_API_KEY_ID --repo DIYgod/pinky --body <key id>
   gh secret set NOTARY_API_ISSUER --repo DIYgod/pinky --body <issuer id>
   ```

   The Apple ID instead also works: `APPLE_ID`, `APPLE_TEAM_ID` (492J8Q67PF) and `APPLE_APP_PASSWORD`, an app-specific password from <https://account.apple.com>.

4. **The companion's release key, `COMPANION_KEYSTORE` and `COMPANION_KEYSTORE_PASSWORD`.** `scripts/companion-key.sh` makes it: a PKCS12 keystore with one RSA 4096 key under the alias `pinky-companion`, a certificate for `CN=Pinky Companion, O=Pinky` valid for 30 years, and a random password (the key's too). The key never touches a file or a command line: it stays in the script's variables and goes through pipes to 1Password (the item "Pinky release: Companion signing key (Android) for GitHub Actions", in the vault `OP_VAULT`, Employee by default, of the account `OP_ACCOUNT`, rsshub) and, once the script has read that copy back, to the secrets. 1Password asks the person to allow its CLI. It prints the certificate and the name Android shows for its signer.

   Done on 8 November 2026: certificate SHA-256 `7A:0F:AA:EF:34:62:6E:B2:9B:88:99:55:8E:A2:FE:49:13:40:12:59:1F:43:8D:6C:B9:4B:6B:9F:16:D6:19:EF`, valid until 8 November 2056; `dumpsys package app.pinky.companion` shows its signer as `signatures:[a9ed84e0]`. The keystore (base64, as the secret holds it) and its password are in the 1Password item, and nowhere else but the secrets.

   Keep that copy: GitHub never shows a secret again, and without the key no release can update the Companions installed until then. To set the secrets from it again: `op read "op://Employee/Pinky release: Companion signing key (Android) for GitHub Actions/keystore" | gh secret set COMPANION_KEYSTORE --repo DIYgod/pinky`, and the same with `password` for `COMPANION_KEYSTORE_PASSWORD`. If the key leaks, run the script again (it makes a new key, a new item and new secrets) and delete the old item: the next release replaces every installed Companion, and each phone's owner sees a notice once.

All three Apple secrets were done on 7 November 2026, with an API key named "Pinky GitHub Actions notary". GitHub never shows a secret again, so the owner keeps the `.p12`, its password and the `.p8` in 1Password, as the "Pinky release: …" items.

**Renewing.** The Developer ID certificate expires on 1 February 2027, together with Apple's first Developer ID certification authority, which issued it. Before then, the team's Account Holder creates a new Developer ID Application certificate (at <https://developer.apple.com/account/resources/certificates>, or in Xcode), and step 2 is done again with it. On this Mac, codesign finds the identity by its name and stops when two certificates share it: remove the old one from the keychains, or set `SIGN` (`RELEASE_SIGN` for `make release`) to the new one's SHA-1. Releases signed and notarized with the old certificate keep opening after it expires, as their signatures carry a timestamp. App Store Connect API keys do not expire: if the key leaks, revoke it in App Store Connect and do step 3 again with a new one.

The secrets reach only workflows that people with write access start (a tag push, or "Run workflow"), never pull requests from forks. The repository variable `MACOS_SIGN_IDENTITY` names another signing identity.

## By hand

```sh
scripts/release.sh next                   # the version the next release gets
scripts/release.sh tag -n                 # what tag would do
scripts/release.sh build                  # a dry run: build HEAD on this Mac into dist/<version>/
NOTARIZE=1 scripts/release.sh build       # the same, notarized
scripts/release.sh trigger v2026.1107.1   # run the release workflow for a tag again
scripts/release.sh publish v2026.1107.1   # build and publish a tag from this Mac
gh workflow run release.yml --ref <branch> -f tag=v2026.1107.1 -f publish=false
                                          # build a pushed branch on GitHub's runners as
                                          # that version; the files stay as the run's artifacts
```

`build` and `publish` check the commit out in a worktree of their own, so nothing uncommitted gets in, and build what the workflow builds into `dist/<version>/` (a dry run's `latest.json` leaves out what it could not build); FFmpeg's and adb's builds are kept in `~/Library/Caches/pinky-release`. They sign with the Developer ID in the login keychain and notarize with the notarytool profile `pinky-notary`, so they need no secrets. A dry run leaves out what it cannot make, with a warning: without nasm (`brew install nasm`) the Intel FFmpeg lacks its assembly, without NSIS (`brew install makensis`) there is no Windows installer, without CMake (`brew install cmake`) Pinky.app carries no adb, and without Docker (OrbStack or Docker Desktop, running) the Windows and Linux packages carry no FFmpeg and no adb. `publish` stops instead, and needs all four (or `STRICT=0`, to publish without them).

`publish` is for when the workflow cannot run (GitHub Actions is down, or a secret has expired). It publishes the same release as the workflow, so cancel the tag's run before you publish the tag from the Mac. It needs the companion's release key, which only the secrets and the owner's 1Password item hold: put the keystore in a file of its own for the run (`op read "op://Employee/Pinky release: Companion signing key (Android) for GitHub Actions/keystore" | base64 --decode > <file>`), set `PINKY_COMPANION_KEYSTORE` to that file and `PINKY_COMPANION_KEYSTORE_PASSWORD` to the item's password, and delete the file afterwards. A dry run (`build`) does without it and carries the companion's development build instead, with a warning.

To build a tag on GitHub and publish nothing, to check the workflow say, run it with publish off: `gh workflow run release.yml --repo DIYgod/pinky -f tag=<tag> -f publish=false`. The files stay with the run, as its artifacts, for three days.

## When something fails

- **The release workflow failed:** open the run in [Actions](https://github.com/DIYgod/pinky/actions/workflows/release.yml) and choose "Re-run failed jobs". Running it again for the same tag is safe: a draft release gets its files replaced, a published one keeps its files.
- **The tag was pushed but no release came:** the run says why (often a missing or expired secret). Fix it and re-run, or `scripts/release.sh trigger <tag>`, or `scripts/release.sh publish <tag>` from the Mac.
- **The cask is behind:** the tap's [workflow](https://github.com/DIYgod/homebrew-tap/actions) runs every 15 minutes; "Run workflow" runs it now. GitHub turns its schedule off after 60 days without a commit to the tap, and "Run workflow" turns it on again.
- **A release is broken:** release a fix. Do not delete or move a published tag; installed apps and the cask follow the newest version.
- **Two releases at once:** each tag runs on its own. The later version becomes the latest release whichever finishes first, and the cask never goes back to an older version.

## Changing the pipeline

The build and publish steps are the scripts here (`scripts/dist.sh`, `scripts/publish.sh`, `scripts/release/`), and each release runs them as they are at its tag. The tap's workflow lives in the tap: change it in `scripts/release/homebrew-tap/` and run `scripts/release.sh setup`. `go test ./scripts/release` tests the versions, the changelog, the tag, the manifest (and that the app reads it, `TestFeedTheAppReads`) and the cask.

Installed apps update from what a release publishes (`docs/notes/updates.md`): Pinky.app from the zips, which must stay signed, notarized and stapled, and a Windows Pinky by running the installer quietly, which [.github/workflows/update-windows.yml](../.github/workflows/update-windows.yml) checks on GitHub's Windows runner whenever the update code, the Windows packaging (`cmd/pinky/mygo.json`, MyGo's version in `go.mod`) or the workflow change.
