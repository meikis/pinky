# Updates

How Pinky keeps itself and its Android companion app current, and tells the person what changed. Code: `internal/update` (feed, download, checks, the swap on macOS (`install_darwin.go`) and Windows (`install_windows.go`, `helper.go`), the work folder both use (`swap.go`), the changelog, state, idle rules), `ui/update.go` (cards, dialogs, Settings, the automatic installs), `cmd/pinky/update.go` (wiring, the daemon, `pinky update`, `pinky update-helper`), `internal/driver/android/companion_update.go` and `internal/gateway/companion_update.go` (the companion).

## The feed

`latest.json` in the newest release of `update.Repo` (`DIYgod/pinky`, one constant; the release pipeline publishes there, see `docs/RELEASING.md` on its branch): `https://github.com/DIYgod/pinky/releases/latest/download/latest.json`. The repository must be public. `PINKY_UPDATE_FEED` overrides the whole URL (tests, a staging feed); plain HTTP is accepted only from this computer.

```json
{
  "version": "2026.1107.2",
  "tag": "v2026.1107.2",
  "date": "2026-11-07T10:00:00Z",
  "notes": "- Markdown, shown on the update card",
  "assets": {
    "darwin-arm64": {"url": "https://github.com/DIYgod/pinky/releases/download/v2026.1107.2/pinky-2026.1107.2-darwin-arm64.zip", "sha256": "<64 hex>", "size": 41234567},
    "darwin-amd64": {"url": "…/pinky-2026.1107.2-darwin-amd64.zip", "sha256": "…", "size": 0},
    "windows-amd64": {"url": "…/pinky-setup-2026.1107.2-amd64.exe", "sha256": "…", "size": 0},
    "linux-amd64": {"url": "…/pinky-2026.1107.2-linux-amd64.tar.gz", "sha256": "…", "size": 0}
  }
}
```

- Asset keys are `GOOS-GOARCH`. An asset whose URL is not HTTPS, whose sha256 is not 64 hex digits or whose size is not between 1 byte and 2 GiB is dropped; the feed itself must parse and name a release version, and is at most 1 MiB.
- The macOS asset is a zip of the signed, notarized and stapled `Pinky.app`, made with `ditto -c -k --keepParent Pinky.app`. Its `CFBundleShortVersionString` must equal `version`.
- Versions are dot-separated integers (`2026.1107.1`, date-based `YYYY.MDD.N`), compared part by part as numbers, missing parts counting as 0; a leading `v` is ignored. `0.0.0-dev` and anything else that does not parse never update and never see an update.
- Requests carry `User-Agent: Pinky/<version> (<os>; <arch>)` and go through the system's proxy (MyGo's `netproxy`), as browsers do.

## When Pinky looks and installs

- The app checks 10 s after its window exists and every 4 hours (`update.CheckEvery`), quietly when that fails (logged). Settings › Advanced has **Check now**, which says what it found.
- A newer release with a build for this computer shows a card above the sidebar's footer: the version, **What's new** (the feed's notes in a dialog), **Update now** and **Later** (hides it for that version for a day). Update now asks first while an agent task, an unlock or another job runs, since the restart stops it.
- **Update automatically** (Settings › Advanced, on by default) installs by itself when `update.Idle` holds at a minute's tick: no agent task running, waiting or paused, no request waiting, no unlock, companion install, pairing or playback under way, nobody in control of a phone, and neither the person (window focus and the system's idle time while focused) nor an agent active for 10 minutes (`update.QuietFor`). Downloading can take a while, so the restart checks again; if someone came back, the card says the update is ready and Pinky restarts at the next quiet tick (or on **Restart now**).
- An automatic install whose download or checks failed is tried again quietly after an hour, then four hours, then daily (a newer release starts afresh). A download that receives nothing for a minute stops (`update.StallTimeout`), so a proxy that hangs does not hold the update for ever. A version that the helper could not swap in, or that did not start, is not installed again by itself (`State.Failed`); Settings says why, and Update now tries again.
- An automatic update brings Pinky back without taking the front, and with its window closed (`--background`) if it was closed. Pinky's own variables (`PINKY_*`, `MYGO_AUTOMATION`) are passed on with `open --env`, which Launch Services would otherwise drop.

## Installing on macOS

`update.Locate` decides from the running executable:

| Where Pinky runs | What the card offers |
| --- | --- |
| a `Pinky.app` signed by team 492J8Q67PF, in a folder the person can write to | Update now: replace and restart |
| a copy macOS translocated (run from Downloads with quarantine) | Download, with "move Pinky to Applications" |
| the disk image, or a folder the person cannot write to, or a bundle owned by another user | Download, with why |
| a build signed ad hoc (`make app` on this Mac) | Download: a developer's build is never overwritten |
| Homebrew's cask (`brew install --cask diygod/tap/pinky`: Pinky.app in Applications) | Update now, as above (see Homebrew below) |
| a Homebrew formula (in the `Cellar`) | the `brew upgrade pinky` command, with Copy |
| not in a bundle (`go run`, a bare binary) | Download |

The install (`BundleInstaller`):

1. Download into `<data>/updates`, checking the size and SHA-256 against the feed.
2. `ditto -x -k` into `.Pinky.app.update-XXXX/new` next to the installed bundle (same volume, so the swap is a rename), then check: exactly one app, `CFBundleIdentifier` `app.pinky.desktop`, the feed's version, an executable, `codesign --verify --deep --strict -R='anchor apple generic and identifier "app.pinky.desktop" and certificate 1[field.1.2.840.113635.100.6.2.6] exists and certificate leaf[field.1.2.840.113635.100.6.1.13] exists and certificate leaf[subject.OU] = "492J8Q67PF"'` (a Developer ID Application certificate of Pinky's team), and `codesign --verify -R='notarized'`: Apple notarized it. That reads the ticket stapled to the bundle (releases are stapled), or one the system already knows, without a network; `codesign --check-notarization` would look it up online, which fails where Apple cannot be reached. Checked on this Mac with the first release (passes, also with the network denied to codesign) and with a copy changed and signed again with the Developer ID (fails in 0.04 s, as `spctl` says "Unnotarized Developer ID"): notarization goes with the code, so a copy signed again without changes still counts as notarized. A bundle of the team that is not notarized fails with `ErrNotarized`, worded on its own on the card. The e2e build (`-tags e2e`, `notarization_e2e.go`) leaves the notarization out, as the end-to-end test signs its bundles but cannot notarize them; release builds never carry the tag. The quarantine flag is removed so the unattended first start does not wait on Gatekeeper's question; the checks above are the trust decision.
3. Write `pending.json` (from, to) into the work folder and start the helper (`/bin/sh`, in a session of its own), then quit.
4. The helper waits for Pinky's pid to exit (a zombie counts as exited: a Pinky that `pinky mcp` started is that shim's child, which now also reaps it), checks the new bundle's signature again with the same requirement, notarization included (it may have waited next to the app since step 2), renames the installed bundle to `work/old.app` and the new one into its place, and starts it with `open`. Renames keep the path, so the Dock tile and Launch Services stay with the app.
5. The new version, once it holds the gateway's lock, writes its pid to `work/launched` (`update.CheckSwap`), and `work/started` once its window is up (`Swapped.Confirm`). The helper then removes the work folder and the old bundle.
6. If the new version exits before `started`, or never launches within three minutes (the gateway may wait on adb as it starts), the helper moves it to `work/failed.app`, puts the old bundle back and starts it. A new version that runs but is slow to come up is waited for, never rolled back under it.
7. Whenever the update does not happen (the signature no longer checks out, a rename fails, the new version did not start), the helper leaves `rolled-back` in the work folder, with why ("didn't start", completing "Pinky <version> …"), and starts the old version, which records the version as failed (`CheckSwap`), with why for Settings ("Pinky 2026.1107.2 didn't start, so Pinky went back to 2026.1107.1"), instead of trying it again at the next quiet minute. The helper never deletes a bundle that is not in place elsewhere: putting the old one back is tried five times, and if it still fails both bundles stay in the work folder and the log says where. `CheckSwap` only takes the work folder of the swap that is under way (one with `old.app`, the newest), so a folder a helper abandoned for the same version cannot catch the new version's `launched`.

`update.log` in the data folder has the helper's lines. Leftover work folders are removed after a day.

## Homebrew

The cask (`brew install --cask diygod/tap/pinky`, `scripts/release/cask.go`) puts the release's zip into Applications, declares `auto_updates true` and adds the `pinky` command as a wrapper that runs `/Applications/Pinky.app/Contents/MacOS/Pinky` by that path, so it follows every swap. Pinky treats the cask's copy like any other Pinky.app (`homebrew` only names a formula in the `Cellar`) and updates it itself. What Homebrew does then (read in Homebrew 7.0.12's `Cask#outdated_version` and `auto_updates_bundle_outdated?`, and its upgrade):

- `brew outdated` and `brew upgrade` leave the cask alone while the app's own version (`CFBundleShortVersionString`, which a swap brings along) is not older than the tap's: after Pinky updated itself they see nothing to do, though `brew info` still names the version Homebrew installed first.
- When Pinky is behind the tap (Update automatically off, or not quiet yet), `brew upgrade` upgrades it (Homebrew's default `HOMEBREW_UPGRADE_AUTO_UPDATES_CASKS`): it quits Pinky (`uninstall quit: "app.pinky.desktop"`, Pinky's own Quit), which stops a task under way, installs the tap's version and opens Pinky again. That is the person's choice; `HOMEBREW_NO_UPGRADE_AUTO_UPDATES_CASKS=1` turns it off.
- `brew upgrade --greedy` reinstalls the cask whenever the tap's version differs from the one Homebrew installed first, which after Pinky's own updates is mostly the same version again, and in the quarter of an hour before the tap catches up with a release can put the previous version back; Pinky then updates itself again.
- `brew uninstall --cask pinky` removes whatever version is in Applications; a swap's work folder next to it (`.Pinky.app.update-*`) is gone within a day.
- Checked on this Mac, where Pinky 2026.1107.1 came from the cask: `brew info` says `auto_updates` and not outdated, and Homebrew's own `Cask#outdated?`, run with the versions faked (`brew ruby`), gives the cases above (the tap's version, the one Homebrew installed, the app's: 1107.2, 1107.1, 1107.2: `brew upgrade` leaves it, `--greedy` reinstalls; 1107.2, 1107.1, 1107.1: upgraded; 1107.1, 1107.1, 1107.2: left alone, `--greedy` too; 1107.2, 1107.1, 1107.3: left alone, `--greedy` puts 1107.2 back). `brew audit --cask --strict` passes.
- The cask asks for macOS 12 as `depends_on macos: :monterey`: Homebrew 7 deprecates the `">= :monterey"` form the first release's cask has, which makes every brew command that loads it (`brew outdated`, `brew upgrade`) print a warning asking to report it to the tap, until the next release's cask replaces it.

## Windows

`latest.json`'s `windows-amd64` is MyGo's NSIS installer (`pinky-setup-<version>-amd64.exe`), which installs per user into `%LOCALAPPDATA%\Programs\Pinky` and names that folder in its entry in Settings › Apps (`HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\app.pinky.desktop`, `InstallLocation`).

| Where Pinky.exe runs | What the card offers |
| --- | --- |
| the folder Settings › Apps names (Pinky's installer installed it) | Update now, and by itself while idle, as on macOS: the installer runs quietly |
| anywhere else (the portable zip, a build) | Download and install: the installer, opened for the person (`MethodInstaller`) |
| an asset that is not an installer | Download, shown in the folder |

The install (`SetupInstaller`, `helper.go`):

1. Download into `<data>/updates`, checking the size and SHA-256 against the feed; the file must be a Windows program (`MZ`). The installer is not signed (the user decided so), so the feed's checksum over HTTPS is the trust decision; Pinky's download carries no mark of the web, so SmartScreen does not ask.
2. A work folder `.Pinky.update-XXXX` next to the install folder (same volume, so the helper moves files with renames) takes the installer and a copy of the running Pinky.exe, which is the helper: Pinky.exe cannot be replaced while it runs, and the installer stops everything that runs from the install folder. `pending.json` and `job.json` (the pid, the folder, the installer, the version, Pinky's arguments and own variables) go there, then Pinky starts the helper (`pinky-update.exe update-helper job.json`, in a process group of its own, out of the job object where that is allowed, without `MYGO_AUTOMATION`, which an e2e build would take for its automation socket) and quits.
3. The helper waits for Pinky to exit, then moves everything in the install folder to `work\old`: the installer finds the folder empty, and the old version stays whole for a rollback. A rename works even on a Pinky.exe that an agent's `pinky mcp` still runs from; one that will not move is tried for five seconds (a scanner may hold it), and everything is put back if it still will not.
4. It saves Pinky's entry in Settings › Apps, runs `<installer> /S /D=<the install folder>` (quietly, into exactly that folder; `/D=` must come last and unquoted) for up to five minutes, and checks that it exited with 0 and that the installed Pinky.exe's version resource is the release's.
5. It starts the new Pinky.exe from the install folder with the old one's arguments (`--background` when its window was closed) and Pinky's own variables. The new version writes `launched` and `started` as on macOS (`CheckSwap`, `Confirm`); it shows What's new.
6. Once `started` is there, the helper moves back what only the old folder had, in subfolders too, as an installer that writes over the folder would leave it (FFmpeg's DLLs that the person put next to Pinky.exe, as the README says), removes the old files and leaves `done`; the new version removes the work folder once the helper is through, trying for a quarter of an hour while an agent's `pinky mcp` still runs the old Pinky.exe from it (a later start removes what is left). Files of the person's that will not go back stay in the work folder with `kept`, and nothing removes that folder.
7. When the installer fails or installs another version, or the new version exits before it is up, or nothing launches within three minutes (or within 15 s of the started process exiting with nothing launched), the helper stops the new version, moves its files to `work\failed`, the old ones back and the Settings › Apps entry as it was, leaves `rolled-back` with why ("didn't install: the installer exited with 2", "didn't start"), and starts the old version, which records the version as failed and does not try it again by itself. As on macOS, it never removes files that are not in place elsewhere: when it cannot put the old version back, both stay in the work folder and `update.log` says where.

Agents stay connected: MyGo's installer stops every process that runs from the install folder (its `CloseApp`, which a quiet install does without asking), but the helper has moved the old Pinky.exe out of it by then, so an agent's `pinky mcp` lives on and reconnects to the new version as on macOS.

Checked on GitHub's Windows runner (Windows Server 2025, `update-windows.yml`, run 42380794859 on 2026-11-07): an installed 2026.1107.1, left in the tray, updated itself to 2026.1107.2 a minute after it started (the installer took 3 s), and the new version showed What's new; an agent's `pinky mcp` session went on (14 tools after the update); 2026.1107.3, which exits as it starts, was rolled back 15 s after the helper started it, with Pinky.exe, Settings › Apps and the failure recorded as expected, and not tried again. The first runs found that the runner's temporary folder has short names (`RUNNER~1`), which the installer keeps in Settings › Apps, so Pinky now compares the long paths.

`pinky update` downloads the installer for the person to run, since it runs from the Pinky.exe the installer replaces; the app installs updates by itself.

## Linux

**Download** saves the release in `~/Downloads` and shows it. A Pinky that dpkg or pacman installed says to update it through the package manager; a Linuxbrew one shows `brew upgrade pinky`.

## The daemon and the command line

- `pinky daemon` checks at start and every 4 hours and logs when a newer version is out, with what to do; it never replaces itself (it may run under launchd, systemd or a package). `--no-update-check` turns the check off.
- `pinky update [--check]` reports and, without `--check`, downloads, checks and installs the update while no Pinky runs (it says to `pinky quit` first otherwise): on macOS the same checks, then the swap at once (`SwapNow`), no helper, holding the gateway's lock during the swap so that no Pinky starts from the bundle meanwhile.
- **Agents stay connected across an update.** Before it quits for an update the app writes `update-restarting` next to `update.json`; a `pinky mcp` whose gateway goes away while that marker is fresh (5 minutes) keeps the agent's stdio open, waits up to 3 minutes for the new gateway, says hello again, replays the agent's `initialize` (with an id of its own, whose answer it swallows) and `notifications/initialized`, and goes on. Requests the old gateway did not answer are sent again, except `tools/call`, which may have acted on a phone: the agent gets an error saying to call the tool again. The new gateway removes the marker. Without the marker (Quit, `pinky quit`), or when the new version does not come back within those 3 minutes, `pinky mcp` keeps the session too and waits, as long as the agent keeps it, for the person to open Pinky again (it looks for the socket every second and never starts Pinky itself: quitting is how the person stops every agent). Meanwhile it answers every request at once: a `tools/call` with a tool error saying Pinky is not running (or, for one the old gateway left unanswered, that Pinky quit during it and it may have acted), `ping` with an empty result, anything else with an error. `TestShimReconnectsAfterAnUpdate` and `TestShimWaitsForPinkyToOpenAgain` check both with fake daemons; on Windows `TestWindowsUpdate` checks it across a real update.

## Pinky Companion

- A phone whose installed companion is older than the bundled APK (`driver.CompanionOutdated`) shows **Update Pinky Companion** on its page, as before; with Update automatically on, the card adds that Pinky updates it when the phone is free.
- The client checks each Android phone's companion every 10 minutes and updates it (`Gateway.UpdateCompanion`, `driver.CompanionUpdater`) when the phone has no task running or ended in the last two minutes (the driver's own use of the app ends a minute after a task), no request, no unlock and nobody in control. The gateway refuses while an agent holds the phone, and holds the phone's action lock while it installs: an agent that connects meanwhile has its actions wait for the update (its first screen may come from uiautomator, since the app is being replaced). Pinky itself does not restart for an update while a companion update runs.
- Since 2026-11-08 the update leaves the accessibility service off, as after any task: Pinky turns it on only while an agent works, and the person may be using the phone while it updates. One that an older Pinky kept on for good goes off after the update, unless another client uses it. A build signed with another key, which `pm install -r` refuses, is replaced: the driver turns its service off, uninstalls Pinky's own package and installs the bundled APK, and the client says so (`CompanionUpdate.Replaced`; a toast, "Reinstalled Pinky Companion on <phone> with Pinky's private signing key", and a What's new line of kind `companion-replaced`). That moves every phone off the builds signed with the public key of the builds until 2026-11-08 (`companion.OldPublicSigner`) by itself, except where Play Protect checks USB installs, where it waits for the person's Update. Before 2026-11-08, the update was `pm install -r` of the bundled APK, Pinky's own package only, and kept the accessibility service as it was. **Android keeps `enabled_accessibility_services` across `pm install -r` of the same package**: checked on the Android 10 emulator (`emulator-5562`) and the Android 16 one with Google Play services (`emulator-5560`, `verifier_verify_adb_installs` 0), the service was still listed and answered right after the install, and the app keeps its own settings (`idle_disable_ms`, how long it stays on without a client: 240000 before, 240000 after). So when the service was on, Pinky only waits for it to answer; it turns it on again (`TurnOn`, the person's path) only if Android dropped it. A service that was off stays off.
- It refuses (and leaves the card to the person) where the phone would show a dialog: Google Play services installed and Play Protect checking adb installs (`verifier_verify_adb_installs` or `package_verifier_enable` not 0; unset counts as on), or an installed build signed with another key (round one's), which has to be uninstalled and replaced. Nothing else on the phone is read or changed.
- Each automatic update shows a toast and goes into What's new ("Updated on Mi 10 Pro to 0.1.0 (130)").

## What's new

- `CHANGELOG.md` at the repository's root is embedded through the root package (`pinky.Changelog`): newest first, `## <version> (<YYYY-MM-DD>)` per release with bullets; headings without a version (`## Unreleased`) are skipped, and `## [2026.1107.1] - 2026-11-07` is read too.
- `update.json` (next to the settings) keeps `last_run`. A start of a newer version sets `notes_from` to the version that ran before; the card **Pinky was updated** opens the entries after it, up to the running version, and the companion updates since. Closing it clears both. A first install shows nothing; Settings › Advanced › What's new shows this version's entries at any time.

## Tests

- `go test ./internal/update/`: versions, the feed (bad assets, redirects to HTTP, size limits), downloads (checksum, size, nothing left behind), the changelog, the state, idle rules, and on macOS staging (wrong version, identifier, two apps, not a zip, an unsigned bundle), the swap with a fake running app and a fake `open` (success, rollback after a crash, rollback when nothing launches), leftovers, `Locate` (translocated, read-only, ad hoc, Homebrew). `PINKY_SIGNED_APP=<an unzipped release>/Pinky.app PINKY_E2E_SIGN="Developer ID Application: … (492J8Q67PF)" go test -run TestSignatureOfARelease ./internal/update/` checks the requirements against a real release (notarized and stapled), a modified copy, and a modified copy signed again with the Developer ID, which is the team's but not notarized.
- `go test -run 'TestHelper|TestMoveAll|TestReadJob|TestSetupLocation|TestCheckSwapOfAnInstallFolder' ./internal/update/`, on every system: the Windows helper with fakes for the installer and the processes (the update, a slow start, the installer failing or installing another version, the new version crashing, exiting before it is up or never launching, Pinky not quitting, the person's files that will not go back), and where a Windows Pinky updates itself.
- `go test ./ui -run 'Update|WhatsNew|Companion(UpdatesWhen|Waits)|SettingsCheckNow|RelaunchArgs'`: the cards and dialogs in `ui.Tester` against a local feed and a fake installer, automatic installs waiting for an agent, the person and the setting, Homebrew and download-only installs, What's new, the companion's automatic update with the fake gateway.
- `scripts/devlock.sh <emulator> env PINKY_DEVICE=<emulator> PINKY_COMPANION_OLD=<older apk> PINKY_COMPANION_APK=<bundled apk> go test -tags device -run TestDeviceCompanionUpdate -v ./internal/driver/android/`: off stays off, on stays on with the app's own settings, and what Android does by itself; it leaves the bundled build installed and the setting and the app's settings as found. Passed on `emulator-5562` (Android 10) and `emulator-5560` (Android 16), with builds 75 and 130.
- `go test -run 'TestShimReconnectsAfterAnUpdate|TestShimWaitsForPinkyToOpenAgain' ./cmd/pinky/`: an agent's session through `pinky mcp` survives a restart marked as an update, and a plain quit until Pinky is opened again.
- `PINKY_E2E_SIGN="Developer ID Application: … (492J8Q67PF)" go test -tags e2e -run TestUpdateEndToEnd -v -timeout 10m ./cmd/pinky/`: two e2e builds signed with the Developer ID in a temporary Applications folder and a local feed; the older one, left alone in the menu bar, updates itself, the newer one starts, drops the old bundle and shows What's new over MyGo's automation socket (in English or Simplified Chinese: the e2e build shares Pinky.app's defaults, so the language picked in Pinky's Settings on that Mac). Passed on this Mac on 2026-11-07 after the notarization and Windows changes, in 71 s, with the person's own Pinky running alongside.
- `.github/workflows/update-windows.yml` (GitHub's Windows runner; on changes to the update code, the Windows packaging or the workflow, and by hand): three e2e builds with their installers, and `TestWindowsUpdate` (`cmd/pinky/update_windows_e2e_test.go`) installs the oldest quietly into a temporary folder, lets it update itself from a local feed (Pinky.exe's version, Settings › Apps, update.json, What's new, the work folder gone), then serves the newest, which exits as it starts (`PINKY_E2E_CRASH_VERSION`, e2e builds only): the helper must put the version before it back, files and Settings › Apps entry, and that version must not try it again. It uninstalls Pinky at the end.

## Open questions

- **Revocation:** the notarization check reads the stapled ticket, offline; a ticket Apple revoked afterwards is not noticed (that takes the online check, which Gatekeeper does at the first start of a quarantined app, and Pinky removes the quarantine flag of its update).

- **With the release pipeline** (agreed 2026-11-07): releases, `latest.json` among them, are published in `DIYgod/pinky` itself, public since that day (Pinky fetches without credentials); `latest.json` is uploaded after the files it names and "latest" only moves forward. Assets: `pinky-<v>-darwin-<arch>.zip` (the stapled Pinky.app), `pinky-setup-<v>-amd64.exe` (the NSIS installer), `pinky-<v>-linux-amd64.tar.gz` (MyGo's archive). Builds from source say `0.0.0-dev` (`make app` too) and never check. Until the first release the feed answers 404, which the app logs and otherwise ignores.
- **Agents during a restart:** an agent connected over HTTP (Settings › Agents › Remote agents) loses its session when the app restarts. The new gateway answers the old session's id with 404 (the MCP SDK's streamable HTTP handler), and the MCP specification has a client then start a new session by itself; clients that do not have to connect again. Agents on `pinky mcp` reconnect (above).
- **The releases repository:** `update.Repo` is `DIYgod/pinky`, as the release pipeline's `docs/RELEASING.md` says; it must stay in step with the pipeline's `defaultReleasesRepo`, and the repository must be public.
- **Windows installer signing:** the installer and Pinky.exe are not signed (decided 2026-11-07); a signature check would join the checksum once they are.
