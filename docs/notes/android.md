# Android driver

Status, round three (2026-11-07): everything the round asked for is built and checked on two emulators, `emulator-5560` (Android 16) and `emulator-5562` (Android 10, standing in for the Pixel). The Mi 10 Pro (LineageOS, Android 17) was used read-only and with the Calculator only, until it locked at about 10:36 with someone apparently at it ([below](#the-mi-10-pro-in-round-three)). The Pixel is still offline. Branch `claude/android-driver-1aff78`, not pushed this round.

## What was built

| Package | What it does |
| --- | --- |
| `internal/driver/android/adb` | A client for the adb server protocol: `host:version`, `host:devices-l`, `host:track-devices-l`, `host-serial:<serial>:features`, `host:transport:<serial>`, `shell:` (v1, interleaved output), `shell,v2,raw:` (stdout, stderr, exit code), `exec:` (binary-safe), `sync:` (push, pull, `STAT`/`LST2`, `LIST`/`LIS2`, several requests on one connection), local sockets, forwards, and wireless debugging: `host:mdns:check`, `host:mdns:services`, `host:pair:`, `host:connect:`. It reuses the running server and never kills one; when none runs, `StartServer` starts one, with Pinky's own adb when the computer has none ([below](#pinkys-own-adb)). |
| `internal/driver/android/scrcpy` | The bundled scrcpy 5.0.1 server (`server/`, with its Apache-2.0 license and checksum) and its protocol: `key=value` options, the video stream headers, control messages, device messages, `start_app` results read from the server log, and `list_apps`. The options that would keep a screen awake or press POWER do not exist and are dropped from raw extras. |
| `internal/driver/android` | The driver. `Provider` (also `driver.Pairer`), `Device` (also `Toucher`, `LockReporter`, `Clipboard`, `Settler`, `PersonWatcher`, `Waker`, `Unlocker`, `Files`, `Backgrounder`, `CompanionInstaller`), and `Background`, the device a background display gives (`Toucher`, `LockReporter`, `Clipboard`, `Settler`, `Files`). |
| `internal/driver/android/mdns` | Pinky's own mDNS search (RFC 6762/6763), questions only: the adb server's discovery stops working after the computer's network changes. See [Wi-Fi without adb's discovery](#wi-fi-without-adbs-discovery). |
| `internal/driver/android/companion` | The Go client for the companion app (owned by this session since round three; the app itself is the companion session's). Round three added `TurnOn`, `KeepEnabled`, `EnabledForTask`, `Uninstall`, `InstalledVersion`, `APKVersion`, `Info.Features` and a display for `set_text`. |
| `internal/driver/android/cmd/androidctl` | A debug command for every feature, with timings. |

Shared contracts: none changed by this session. `cmd/pinky/companion.go` (gateway-core's) now calls `SetUpCompanion`, because `InstallCompanion` had to take the contract's signature.

### How it works

- **One scrcpy session per device** carries input, the H.264 stream and settle detection. It starts on first use (about 0.5 s on the Mi), restarts by itself on the next call after it dies, and connects to the server's abstract socket straight through the adb server, so no host port is used. Each run pushes its own jar, which the server deletes once it starts; a server that crashed before that (the Android 10 emulator's failing encoder left six) has it removed by the driver when the session ends, and `Close` waits up to 3 s for that.
- **Server options Pinky always sets:** `audio=false`, `tunnel_forward=true`, `clipboard_autosync=false`, `power_on=false`. Never: `stay_awake`, `power_off_on_close`, `turn_screen_off`, `screen_off_timeout` (the options are gone from the code, and `Options.Extra` drops them). The phone's own screen timeout and lock always apply.
- **When the video encoder fails,** the device runs a session without video instead. The Android 10 emulator's encoder fails at once (`Capture/encoding error: IllegalStateException`), and so does the image's own `screenrecord` (`Encoder failed (err=-38)`), with and without SwiftShader. The first video session of a device waits up to a second for its first frame; if the server reports an encoder failure instead, the driver logs it once and starts a control-only session: taps, keys, typing, the clipboard and unlocking keep working, touches go in display pixels (the size from `dumpsys window displays`, `cur=WxH`), settle uses the companion's events (300 ms quiet) or a fixed 1 s wait, and `Stream` and background displays return `driver.ErrUnsupported`. `Stream` tries video again after a minute.
- **Input** (taps of 50 ms, long presses of the phone's `long_press_timeout` plus 400 ms, swipes, raw touches, keys) goes through scrcpy's control messages. **One gesture at a time per session:** a session restart (a `Stream` with other options) waits for the gesture in progress plus 150 ms for the server to read it, and a gesture that finds its session replaced before it began goes to the new one. This fixed the lost taps (below).
- **Wake (`driver.Waker`).** `ScreenOn` reads display 0's power state from the display power controllers (`dumpsys display`: the `mScreenState` that follows `mDisplayId=0`; Android 10 has one controller). The overall wakefulness and the device idle controller's `mScreenOn` both say "on" while a background display keeps its own display group awake, so neither is used. `Wake` presses `KEYCODE_WAKEUP` (never POWER) when the screen is off and waits until it is on; it never unlocks. Actions keep waking the screen as before.
- **Unlock (`driver.Unlocker`).** Wake, then `wm dismiss-keyguard`, which shows the PIN pad on a secure lock screen. The alternate bouncer of phones with an under-display fingerprint sensor (`alternate_bouncer`, `udfps_touch_overlay`) gets a tap above the sensor, which brings up the PIN pad; otherwise a swipe up from 70 % of the height is the fallback, but never on a screen that shows digit keys the driver cannot read (on some pads a touch on a key enters its digit). The driver enters nothing until a tree read (the companion's, or uiautomator's when the companion is off) shows one of these, and then enters the PIN through scrcpy, never in a shell command line (where other processes could read it), and never logs it:
  - **AOSP's PIN view or password field** (`pinEntry`, `passwordEntry`), or a focused password field in SystemUI (with digit keys for a PIN): the PIN goes in as key codes, as one gesture, 80 ms apart. A 300 ms pause follows, then Enter unless the phone already unlocked itself (Android's auto-confirm).
  - **Otherwise, a PIN pad read from its keys' labels and layout** (`pinpad.go`), for OEM lock screens and SystemUI's Compose bouncer: ten keys in SystemUI or a keyguard package (MIUI's is `com.android.keyguard`), each labelled with one digit in its text or description ("2", "2 ABC", "2\nABC", the word "Two" as on vivo's pads, Arabic-Indic or full-width digits), laid out as a phone keypad (three evenly spaced columns, 1 to 9 in three evenly spaced rows and one key below the middle column; the digits may be scrambled), no key bigger than its cell, all on one window, none under a window above it, exactly one such grid on the screen, and in the same place in two reads in a row: a pad still sliding in reads a few dozen pixels off, and gets 2 s from when it first shows to settle. A key without a label may take its digit from its resource id (`key0`, `VivoPinkey0`) only when every key with both a label and such an id agrees, because LineageOS's scrambled pad keeps the ids in place and moves the labels. The PIN goes in as taps on the keys' centers, 150 ms apart, with a lock check before each digit after the fourth (a pad that checks the PIN as it is typed may have unlocked, and further taps would land on the screen behind). If the phone is still locked 0.6 s after the last digit, the driver reads the pad again and, when it is unchanged and shows a confirm key in its bottom row or just below it (`key_enter`, `btn_letter_ok`, "OK", "Enter", "确定", "✓" and the like), taps it after one more lock check; pads without one check the PIN by themselves.

  Then up to 4 s for the lock screen to go. Still locked: `driver.ErrLocked`, never retried; an error after the first digit went in is `ErrLocked` too, since those digits would spoil another try. Nothing entered: `driver.ErrPINNotEntered` (no PIN pad, a pad the driver cannot read with confidence, keys disabled after too many attempts, a saved PIN that is not all digits on a PIN pad). A pattern lock is `ErrUnsupported`. Which lock screens are checked on a device and which only in tests: see [Unlocking other Android phones](integration.md#unlocking-other-android-phones).
- **The Pinky folder (`driver.Files`)** is `/sdcard/Pinky`, over the sync protocol: `ListFiles` walks it with `LIS2` (or `LIST` on phones without `ls_v2`), folders included, sorted, and creates it when missing; `Pull` and `Push` take paths relative to it. Paths are cleaned and refused with `driver.ErrNotFound` when absolute, with any `..`, a backslash, control characters or nothing left. Every component is checked with `LST2`/`STAT` (lstat), so a symbolic link is never followed. `/sdcard` refuses to create links on both emulators anyway. `Push` creates folders, keeps the modification time, checks the size, and on Android 10 tells the media scanner (Android 11 and later index files written through `/sdcard` by themselves).
- **Background displays (`driver.Backgrounder`).** `OpenBackground` opens a scrcpy virtual display at the main display's size without system decorations, with the keyboard kept on it, and starts the app. The device it returns streams, observes (through the companion), takes input and settles on that display only, and closes it on Close. **The person's tasks are never destroyed:** starting an app the person already has a task for moves that task to the virtual display (Android reuses it), so before every start the driver notes which tasks are elsewhere (`cmd activity stack list`), and Close moves those back to the main display (`cmd activity display move-stack <id> 0`) before the display goes and takes only the agent's own tasks with it. A task given back lands on top of the main display: Android has no shell command to put it lower. `DisplayOptions.KeepContent` (`androidctl bg -keep`) moves every task to the main display instead, also on top. From Android 13 the display is always unlocked and has its own display group, so it keeps working while the phone is off and locked ([below](#background-displays-while-the-phone-is-off-and-locked)); there `Background.Locked` is false, and the display gets a key that does nothing (`KEYCODE_UNKNOWN`) every 4 s without other input, which keeps its display group awake through the lock screen's 10 s timeout. Its reads turn the companion on again at once after a failure (there is no uiautomator to fall back on there) and retry once. `Type` uses the companion's `set_text` on that display when the app has the `displays` feature, and settle waits for that display's events too.
- **Companion app.** Its accessibility service is on only while an agent works on the phone (since 2026-11-08): the phone's apps can see an accessibility service that is on, and banking apps refuse to sign in next to one that is not from an app store. The gateway tells the driver when an agent works (`driver.WorkWatcher`: a lease is held and the person is not in control). `AgentWorking(true)` turns the service on in the background (`Enable`), so the task's first read finds it ready; `AgentWorking(false)` turns it off 5 s after the last driver call (`companionRest`), and while an agent works a minute without driver calls does; Close does too. Turning it off restores the setting exactly when the driver turned it on, and otherwise removes only Pinky's entry: a service an older Pinky kept on for good (`idle_disable_ms` 0) goes off as well, at the first `AgentWorking(false)` after the device opens. While no agent works, Observe and Type do without the companion (uiautomator, the keyboard) and never turn it on; unlocking from the computer (`readLockNodes`) and background displays still do. Callers that never call `AgentWorking` (androidctl, tests) turn it on at the first read and off after a minute. Several drivers (two Pinky processes, Pinky and `androidctl`) can share it: one leaving keeps the service on while another client is connected, and the last one to leave turns it off. The build used is the one Pinky carries (`companionApp`): the release build `app.pinky.companion` in releases, the development build `app.pinky.companion.dev` in builds from the source. Events are tracked per display (`display` in each event; -1 counts for every display).
- **The companion installer (`driver.CompanionInstaller`).** `CompanionState`: `missing` (or `no_apk` when Pinky has no APK either); `outdated` when the installed versionCode is lower than the bundled APK's (`assets/pinky-companion.json` in the APK; versionCode is the commit count) or the installed build is signed with another key (`dumpsys package` names its signer, `signatures:[dd80ed25]`, Java's hash code of the certificate, which `companion.APKVersion` computes for the bundled APK from its signing block); `on` while the service is on (an agent works, or another client has it on); `off` when it did not come on the last time the driver tried (the phone refuses settings changes over USB, say); else `ready`: Pinky turns it on while an agent works. `InstallCompanion` installs when the phone has none, an older build or one signed with another key, which cannot be updated in place: the driver turns it off, uninstalls Pinky's own package and installs the new one (also when `pm install -r` says `INSTALL_FAILED_UPDATE_INCOMPATIBLE`). It then turns the service on through the driver's own link to check that it comes on, and it goes off again 5 s later unless an agent works. Until 2026-11-08, Install turned the service on for good (`TurnOn`, `idle_disable_ms` 0). Play Protect may make the phone ask the person; the call waits up to its context.
- **Pairing (`driver.Pairer`, on the Provider).** `StartPairing` makes a random service name (`pinky-xxxxxxxxxx`) and password, `QR()` returns `WIFI:T:ADB;S:<name>;P:<password>;;`, and `Wait` looks for the phone's `_adb-tls-pairing._tcp` service of that name both in the adb server's mDNS services and in the driver's own search (`Config.Browser`, package `mdns`), pairs with `host:pair:<password>:<addr>`, and waits up to 30 s for the phone to come online. A server whose discovery lists the phone may connect by itself (serial `adb-<serial>-<suffix>._adb-tls-connect._tcp`) and gets a second for it; otherwise the driver connects at once to the phone's `_adb-tls-connect._tcp` service, from either list, found by the guid `host:pair` reports, or by the address it paired from. `PairWithCode` does the same with the address and six-digit code the phone shows. A pairing reports its progress (`driver.PairingReporter`: found, paired, and whether the system blocks the search). When the phone paired but its connect service shows up nowhere, the error wraps `driver.ErrPairedNotFound`, and `ConnectWiFi` (`driver.WiFiConnector`) connects at the address Wireless debugging shows. A real adb server (platform-tools 37) answers `host:pair` with OKAY and then a second OKAY or FAIL once the pairing is over; `Pair` reads both forms. Pairing returns the phone by the id `List` gives it (over Wi-Fi its hardware serial); before 2026-11-07 it looked it up by the connection's serial and failed with not found after a real pairing.
- **Reconnecting over Wi-Fi (`Config.ReconnectWiFi`, on in the app unless `PINKY_DEVICES` is set).** `List` starts a pass in the background when one is due: the guids in the adb server's `~/.android/adb_known_hosts.pb` (the phones it paired with) that have no connection in use and that the server's discovery does not list are searched for on the network for up to 6 s, and the ones found are connected by address (`host:connect`). An address that refused is tried again after 5 minutes, then 15, 45 and so on up to 6 hours (at once at a new address); when the server is gone after the attempt, after 6 hours. The adb server of platform-tools 37 can crash when a TLS handshake fails, and every other tool loses its connections with it. Passes come 5 s after a phone came or went, then back off from 10 s to 2 minutes while a paired phone stays away, and once a minute while all are there. A computer that never paired a phone over Wi-Fi never searches.
- **Person input** (`PersonInput`): `getevent` on the touchscreens and hardware-key devices from `getevent -lp`: direct multi-touch devices, or, on a phone without any (the emulators' virtio touchscreens have no input properties), its multi-touch devices; devices with power, volume, home, back, menu or recents keys; never `uinput` devices. Injected input never reaches /dev/input, so it is not reported. At most one signal per 300 ms.
- **Observe:** the companion's tree when it is on, else `uiautomator dump`; lock and keyboard state from the window manager in parallel. The screen conversion lists an input method window's elements whenever the app sends its nodes (build 70 sends them unasked for a keyboard sheet that covers the screen, such as Gboard's stylus tutorial), and hides what lies under a higher window by its touchable `region` when the app sends one (Android 13+), else by its bounds: Gboard's stylus toolbar spans the lower half of the screen but covers only a narrow strip. When a tree is still empty while the keyboard shows (older builds, such as the Mi's), the driver reads again with the keyboard's window, then falls back to uiautomator for that read.

### Settle detection

As in round two: a video packet counts as a change unless it is a refinement (85 to 125 ms after the previous capture), a key frame or configuration, or tiny; the screen has settled when no change came for 400 ms (limit 3 s). While the companion is on, its window and content events must also have been quiet for 300 ms; since round three only the events of the display being settled count (and those whose display is not known). Without video: the companion's events, or a fixed 1 s.

## Using it from the gateway

```go
p := android.NewProvider(android.Config{Logf: log.Printf}) // driver.Provider and driver.Pairer
dev, _ := p.Open(ctx, id)                                  // *android.Device
dev.(driver.Waker).Wake(ctx)                                // before an agent's step on the main screen
bg, _ := dev.(driver.Backgrounder).OpenBackground(ctx, driver.BackgroundOptions{App: "com.android.settings"})
```

- **Do not wake the phone for an agent working on a background display:** that would turn the screen on, which is what background work avoids. `Background` does not implement `Waker` on purpose.
- `Background.Locked()` is false from Android 13, and so is its `Screen.Locked`; the phone's own `Device.Locked()` still says whether the phone is locked. Agents on a background display are not stopped by the lock screen: the gateway's policy (blocked apps, approvals) is all that stands between them and the phone's apps.
- `Unlock(ctx, pin)` returns nil, `ErrLocked` (the PIN went in, all or part of it, and the phone is still locked: do not try again), `ErrPINNotEntered` (nothing was entered: no PIN pad, one the driver cannot read with confidence, attempts refused), `ErrUnsupported` (pattern, characters it cannot type) or `ErrOffline`. Errors never contain the PIN.
- `Files` paths are slash-separated and relative to `/sdcard/Pinky`; `ErrNotFound` for anything outside or missing. `Background` serves the same folder.
- `CompanionState` costs one `dumpsys package` (7 to 80 ms), plus `settings get` and a ping when the app is current. `InstallCompanion` may take minutes on a phone where Play Protect asks.
- `func (d *android.Device) SetUpCompanion(ctx context.Context, force bool) (android.CompanionInstall, error)` does what `InstallCompanion` does and reports `APK`, `Bundled` and `Before` (versions), `Installed` and `Replaced`; `force` also installs over the same versionCode when the file differs. `android.FindCompanionAPK()` looks at `$PINKY_COMPANION_APK`, `pinky-companion.apk` next to the executable, `../Resources/pinky-companion.apk` (macOS bundle), then the debug build under the working directory.
- `Stream` returns `ErrUnsupported` on a phone whose encoder fails; input still works there.
- **`cmd/pinky`'s `onlyProvider` (`PINKY_DEVICES`) hides `driver.Pairer`:** it embeds `driver.Provider` only, so `Gateway.StartPairing` finds no pairer while that variable is set. gateway-core may want to forward it. (Since forwarded, with `driver.WiFiConnector` and the pairing's progress.)
- **The app sets `ReconnectWiFi`,** except while `PINKY_DEVICES` limits it to some phones (the others belong to other tools on the shared server). Other providers (`androidctl`, `cmd/pinky/companion.go`, tests) leave it off, so they never search the network unasked; pairing searches whatever the setting.

## How to run

```sh
go build -o /tmp/pinky-android/androidctl ./internal/driver/android/cmd/androidctl
A=/tmp/pinky-android/androidctl
scripts/devlock.sh emulator-5560 $A -s emulator-5560 screen          # true or false
scripts/devlock.sh emulator-5560 $A -s emulator-5560 wake
scripts/devlock.sh emulator-5560 env ANDROIDCTL_PIN=1234 $A -s emulator-5560 unlock   # or the PIN on stdin
scripts/devlock.sh emulator-5560 $A -s emulator-5560 files
scripts/devlock.sh emulator-5560 $A -s emulator-5560 push notes.txt docs/notes.txt
scripts/devlock.sh emulator-5560 $A -s emulator-5560 pull docs/notes.txt /tmp/pinky-android/notes.txt
scripts/devlock.sh emulator-5560 $A -s emulator-5560 companion-state
scripts/devlock.sh emulator-5560 env PINKY_COMPANION_APK=app-debug.apk $A -s emulator-5560 companion-install
scripts/devlock.sh emulator-5560 $A -s emulator-5560 bg -app com.google.android.deskclock state sleep:5000 state observe
```

`bg` runs steps on a background display; its `state` step prints whether the phone's own screen is on and locked. Never pass a PIN on the command line.

Wi-Fi, against a second adb server whose discovery is off, which stands in for one whose discovery stopped and leaves the shared server (and its emulators) alone:

```sh
ADB=~/Library/Android/sdk/platform-tools/adb
ADB_MDNS=0 ADB_USB=0 ADB_EMU=0 $ADB -P 5038 start-server   # no discovery, no USB, no emulators
export ADB_SERVER_SOCKET=tcp:127.0.0.1:5038
$A wifi-search -seconds 8                                  # what Pinky's own search sees
scripts/devlock.sh 3e8be48e $A -v pair-qr /tmp/pinky-android/qr.png   # scan it on the phone
scripts/devlock.sh 3e8be48e $A -v pair-code 192.168.5.137:37109 123456
scripts/devlock.sh 3e8be48e $A wifi-connect 192.168.5.137:38161
scripts/devlock.sh 3e8be48e $A -v wifi-reconnect -seconds 60          # $ADB -P 5038 disconnect … meanwhile
$ADB -P 5038 kill-server
```

On macOS the binary needs Local Network access ([below](#macos-local-network-privacy)); sign it once with an identifier (`codesign -s - -f --identifier app.pinky.androidctl $A`) and keep that build.

Tests:

```sh
go test ./internal/driver/android/...       # unit tests, fake adb servers and fixtures
scripts/devlock.sh emulator-5560 env PINKY_DEVICE=emulator-5560 PINKY_TEST_PIN=1234 \
  PINKY_COMPANION_APK=/path/to/app-debug.apk go test -tags device -v ./internal/driver/android/
```

On a phone the device tests use only the home screen, Settings search and the Calculator, and skip what needs an emulator. On emulators they also press power, unlock with `PINKY_TEST_PIN`, use the phone app's keypad where there is no Calculator, install the companion and change the accessibility setting (put back afterwards). `PINKY_TAPS` sets the taps per batch of `TestDeviceTapsLand` (40).

## Wi-Fi without adb's discovery

2026-11-07. The owner's Homebrew Pinky 2026.1107.1 sat on "Pairing…" on both the Mac and the Mi: the adb server listed no mDNS service at all, while `dns-sd -B _adb-tls-pairing._tcp` showed the phone's. Restarting the server fixed it at once, as it had that morning for a server running since 5 November.

**Why the server stops seeing the network.** Its log (`$TMPDIR/adb.501.log`) shows the server of 15:24 reconnecting the Mi over Wi-Fi at 15:24, 15:28 and 16:07, then losing it at 21:18 and never finding it again. At 21:08:40 an iPhone was plugged in: macOS attached its USB network interfaces (`en10`, `anri0`) and configd logged "network changed" at 21:09:20. The server's discovery (openscreen, `mdns daemon version [adb discovery 0.0.0]`) does not survive such a change, and stays blind until it restarts. Plugging in an iPhone, a VPN or a sleep is enough, so on a computer that runs for days it is the normal case.

**What Pinky does now.** It searches for `_adb-tls-pairing._tcp` and `_adb-tls-connect._tcp` itself (package `mdns`) whenever it pairs, and in the background while a paired phone is away, and connects by address; see How it works. It never restarts the server. In the logs: "the adb server's discovery does not list …; Pinky found it at …".

**How the Mi answers mDNS** (LineageOS 24, Android 17, measured from the Mac):

- A question with the unicast-response bit from another port than 5353 gets its answer straight back, with the SRV, TXT, A, AAAA and NSEC records in the additional section. A plain question gets a multicast answer (which the group socket reads); a plain question sent straight to the phone gets a multicast answer too. It does not answer a legacy unicast question (`dig @224.0.0.251 -p 5353`) with its id.
- Asleep, it misses the first questions: answers came from the second to the fourth (1–5 s), and at times it answered nothing for a minute. The search asks at 0, 1, 2, 4, 6 and 10 s, then every 5 s.
- After the pairing dialog closes, its pairing service says goodbye (TTL 0) and leaves the search's list at once.

### macOS Local Network privacy

Searching needs Local Network access (macOS 15 and later), and the system gives no error when it is missing: sends succeed and nothing comes back. What was seen on this Mac (macOS 27.2):

- It is decided per executable. Go builds carry the ad-hoc identifier `a.out`; a binary that searched fine stopped receiving anything when copied to another path or rebuilt, while `/usr/bin/python3`, the `dig` binary and a build re-signed with `--identifier app.pinky.androidctl` were let through. TCP connections to the phone worked from every binary. Test binaries built by `go test` are blocked, so `TestDeviceBrowse` reports `mdns: not allowed…` here; the unit tests use the loopback only.
- A process the app starts (the adb server, when Pinky starts it) counts as the app.
- `nehelper` keeps a rule for `app.pinky.desktop`; whether the owner's Pinky is allowed was not checked. Pinky.app now explains its prompt (`NSLocalNetworkUsageDescription`, English in `cmd/pinky/mygo.json`, Chinese in `cmd/pinky/resources/darwin/zh-Hans.lproj/InfoPlist.strings`).
- The search tells a blocked process by its own questions: they loop back to its group socket within milliseconds when allowed (multicast loopback) and never when blocked. After 1.5 s without one, `Err` wraps `mdns.ErrNotAllowed`, the pairing reports `NotAllowed`, and Add phone offers Privacy & Security › Local Network.

### Checked on the Mi (owner at the phone)

With a second server on port 5038 with discovery off (How to run), `androidctl` from this branch:

| What | Result |
| --- | --- |
| `wifi-search` | The Mi's connect service in 1.4–5.2 s |
| `wifi-reconnect`, nothing connected | Connected the Mi by address in 6.2 s, listed as `3e8be48e` (its hardware serial) |
| The same, connection dropped (`adb -P 5038 disconnect`) | Back 10 s and 18 s later (5 s to the pass, then the search; a dozing Mi missed one search) |
| `pair-qr`, scanned on the Mi | Found the pairing service, paired in under 0.25 s, connected 0.6 s later; returned as `3e8be48e` |
| `pair-code` with the dialog's code and address | Paired and connected in 2.4 s |
| `wifi-connect` | The right address at once; a closed port: "failed to connect to '192.168.5.137:5555': Connection refused" |

The shared server on 5037 kept its connections to the Mi and `emulator-5560` throughout; the server on 5038 was stopped afterwards. Once the server on 5038 crashed when a reconnect's TLS handshake failed (`Handshake failed in SSL_accept/SSL_connect`, then `libc++abi: … mutex lock failed`), an adb bug: hence the long waits after a refusal. Run every `adb -P 5038` command with the three variables: a bare one starts an ordinary server there, which connects the emulators and the Mi too (it happened once, for three seconds). The Mi was paired with this Mac twice more (QR, code), which only adds this Mac's key to its paired devices.

## Pinky's own adb

Since 2026-11-08 every package of Pinky carries an adb of its own, so that a person with an Android phone installs nothing else (branch `claude/bundle-adb-3b0832`).

**What it is.** `scripts/adb.sh` builds adb, and nothing else, from [android-tools](https://github.com/nmeum/android-tools) 37.0.0p1, AOSP's `android-17.0.0_r1` with a CMake build (the same version as Google's platform-tools 37.0.0), with `scripts/adb/CMakeLists.txt`. It links protobuf 3.21.12 (the last without Abseil), BoringSSL, libusb, fmt, brotli, lz4, zstd and zlib statically, so it needs only the system's libraries; every source is checked against its SHA-256. `adb version` says `Version 37.0.0p1-pinky`. It has no mDNS (android-tools builds that with Rust): Pinky finds the phones on the network itself ([above](#wi-fi-without-adbs-discovery)) and pairs and connects by address, and a server without discovery answers `host:mdns:check` with a failure, which pairing already handles.

| Package | Where | Built |
| --- | --- | --- |
| Pinky.app (arm64, amd64) | `Contents/Helpers/adb`, signed with the app (with the hardened runtime and a timestamp under a Developer ID) | Apple's clang for macOS 12 and later, 6.3 MB, the system's zlib; USB through libusb, adb's default on macOS |
| Windows installer and zip | `adb.exe`, `AdbWinApi.dll` and `AdbWinUsbApi.dll` next to `Pinky.exe` | a 32-bit `adb.exe` (8.4 MB), as Google's: AOSP builds AdbWinApi for 32-bit Windows only, and the two DLLs are AOSP's own (platform/development, `host/windows/prebuilt/usb`, Apache-2.0, checked by SHA-256 and the git blob). Cross-compiled with Ubuntu 24.04's mingw-w64 (posix threads, `-static`, SSE2) in Docker, with MSYS2's mingw fixes (`scripts/adb/windows.patch`). It imports Windows' own DLLs and AdbWinApi.dll only |
| Linux `.tar.gz` and `.deb` | `adb` next to `pinky` (`/opt/pinky/adb`) | a static musl executable (8.8 MB) built in Alpine 3.22, which runs on any x86-64 Linux |

Licenses and build notes go with it (`adb-NOTICE.txt`, `adb-BUILD.txt`; in Pinky.app under `Contents/Resources`): Apache-2.0 for AOSP's code, BoringSSL's, the BSD licenses of protobuf, lz4 and zstd, MIT for brotli and fmt, zlib's, and the LGPL 2.1 of libusb, which is linked statically; its source is in android-tools' release and `scripts/adb.sh` builds and links it again.

**Which adb starts the server.** Pinky uses whatever adb server runs and never kills or restarts one. When none answers, `adb.StartServer` starts one with the first adb that runs and is platform-tools 30 or later (an older one, such as Ubuntu 22.04's 29.0.6, lacks Wireless debugging: it is passed over for a newer one, and used only when none works):

1. the one `PINKY_ADB` names, and only that one: a path, or `pinky` for Pinky's own (no version check);
2. the Android SDK's: `ANDROID_HOME`, `ANDROID_SDK_ROOT`, then where Android Studio puts it (`~/Library/Android/sdk`, `%LOCALAPPDATA%\Android\Sdk`, `~/Android/Sdk`), so that Pinky and Android Studio share one server of one version;
3. Homebrew's (`$HOMEBREW_PREFIX/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, Linuxbrew's): an app opened from the Finder or the Dock has the system's PATH, without them;
4. those on PATH, in order (a system package's `/usr/bin/adb`, Scoop's, Chocolatey's);
5. Pinky's own, last, also when its folder happens to be on PATH.

An adb the person installed comes first so that their own adb commands find a server of their version. Pinky's own runs from a copy in the user's cache folder (`~/Library/Caches/Pinky/adb/<hash>/`, `%LOCALAPPDATA%\Pinky\adb\<hash>\` with the two DLLs, `~/.cache/pinky/adb/<hash>/`; `$PINKY_HOME/cache/adb` when that is set), named after the files' SHA-256, and other versions' copies go: the server outlives Pinky, and running from the app's folder it would keep the Windows installer from updating Pinky (MyGo's installer stops every process started from the install folder, which would take every tool's adb connections down) and a disk image from being ejected.

**How the server starts.** Pinky runs the server process itself, `adb -L tcp:<port> fork-server server --reply-fd <fd>` (what `adb start-server` runs once it has found no server), detached (its own session; on Windows `DETACHED_PROCESS` and an inherited pipe handle), and waits for the `OK` it writes once it has looked for devices. `adb start-server` would check the version of a server that came up meanwhile and kill it when it differs; a server process that cannot listen because another one does only exits. The port is `ServerAddr()`'s, so `ANDROID_ADB_SERVER_PORT` and `ADB_SERVER_SOCKET` hold as before; a server address that is not this computer's is never started.

**When the server goes away.** The Android provider now exists without a server, even without any adb (a development build on a computer without one: it then lists no phones, without an error), and `List` starts one (`Config.StartServer`): at once the first time, and when a server that ran went away, after it has stayed away for 3 s, so that whoever restarts it comes first. An adb client of another version (an old `adb` on PATH, a newer SDK) prints "adb server version (41) doesn't match this client (39); killing..." and starts its own at once; Pinky then uses that one, and pairing and other features an old server lacks fail as they would. After a failure `List` waits 5 s, doubling to two minutes. Since every platform-tools from 29 (2019) speaks protocol 41, as Pinky's does, mismatches come only from much older or much newer adbs.

**USB on Windows and Linux.** Windows needs a driver for a phone's USB debugging interface; adb cannot see a phone without one. `List` reads the USB devices from SetupAPI (`usbdriver_windows.go`) and lists a phone whose debugging interface (compatible id `USB\Class_ff&SubClass_42&Prot_01`) has no driver (problem 28 or 1, or no service) as `StateNeedsSetup`, id `usbdriver:<instance id>`, named as the phone names itself (the bus-reported description of the composite device), with the driver to install: the Google USB Driver for Google's vendor id, Samsung's for Samsung's, the makers' page otherwise. A phone online over Wi-Fi by the serial its USB device reports is left out. Linux needs udev rules for adb to open the USB device; a phone adb reports with "no permissions" says which package has them. Both reach the client as any phone that needs setup does (Add phone's "Found …, but …", the phone's page, the checkup), with `Info.Setup` `usb_driver` or `usb_permission` (the onboarding branch's `driver.SetupUSBDriver` and `driver.SetupUSBPermission`).

**What the computer lacks (`driver.HostChecker`).** `Provider.CheckHost` answers from what the last `List` found, for the checkup page: `adb` when there is no adb to start a server with, `adb_server` when no server runs and starting one failed (Detail has the error), `adb_old` when the server that runs speaks a protocol older than 41 (platform-tools before 29), and `adb_restarts` when the server went away three times or more within ten minutes, as when another program keeps restarting it with an adb of another version. `List` asks a server that came for its version once.

**How it was checked (2026-11-08).**

- `make check`; unit tests for the search order on all three systems, version parsing and the platform-tools 30 limit, the cache copy, starting a server with the test binary as a fake adb (on Windows through the inherited handle, in CI), that a second server where one listens quits and leaves the first, `List` starting a server and waiting after one went away, the Windows driver listing from data shaped as SetupAPI gives it, and SetupAPI itself on the Windows runner (`TestSystemUSBNodes`).
- This Mac, under `scripts/devlock.sh emulator-5560`: `pinky daemon` with an empty `HOME` (no SDK), PATH `/usr/bin:/bin:/usr/sbin:/sbin` (no adb), `PINKY_ADB=pinky` (Homebrew's adb is passed over too), a server port of its own (5039) with `ADB_USB=0 ADB_EMU=0`, only emulator-5560 registered (`host:emulator:5561`) and `ADB_VENDOR_KEYS` pointing at the usual key. The daemon logged `android: started an adb server adb=…/cache/adb/64df412b2df9aab8/adb from=Pinky version=37.0.0p1-pinky`, `pinky devices` listed `sdk_gphone64_arm64 emulator-5560 android usb Idle`, and `androidctl` took a 1080×2400 screenshot (652 ms) and read the home screen through that server. Without `PINKY_ADB`, the same environment started Homebrew's adb (37.0.2). The shared server on 5037 and its devices were the same before and after.
- `scripts/dist.sh darwin` on this Mac (`SIGN=- NOTARIZE=0`) put adb into both apps, with its notes, and `TestPackagedADB` passed with the Apple silicon app unpacked from its zip.
- `TestPackagedADB` (`PINKY_ADB_APP=<package>`) starts a server with a package's adb on a port of its own and asks it for its version and devices; the release workflow runs it for the Linux, Windows and Mac packages (the Intel app's where the runner has Rosetta). The Intel Mac adb could not run here (no Rosetta on this Mac); the Windows adb ran under Wine (`adb.exe version`).
- GitHub, a release run that published nothing (`release.yml` on a temporary branch, as `v2026.1108.90`, run 42401184322): every package was built with its adb, both Pinky.apps were signed and notarized with it, and `TestPackagedADB` passed with the Linux package's (server up in 46 ms), the Windows zip's on the Windows runner (423 ms: the 32-bit adb.exe with AOSP's DLLs, started detached with the inherited pipe), and both Mac apps' (Apple silicon, and Intel under the runner's Rosetta; about 3 s each, as the server waits for its USB scan). CI passed the new tests on Linux and Windows; there, four tests of rules and settings fail on main as well.

**Not done.** mDNS in Pinky's adb (Android Studio's "Pair devices using Wi-Fi" through a server Pinky started fails; it works once that server is gone and Android Studio starts its own); the Windows driver listing has not met a real phone without a driver; no Windows ARM64 or Linux arm64 adb (Pinky has no such packages either).

## Round three results

### Background displays while the phone is off and locked

This is the question behind the owner's keep-awake worry. **On the Android 16 emulator with a PIN, an app on a background display keeps running, drawing, taking taps and text, and streaming while the phone's own screen is off and locked.** Each run used the Clock's stopwatch or Settings on a scrcpy virtual display:

| Run | Result |
| --- | --- |
| Locked before the display opened (screen off, keyguard showing) | Stopwatch reset, started, read 7 s then 11 s, paused at 11 s, by taps on the display. 624 frames in 17.8 s (35 fps). The phone stayed screen off and locked throughout. |
| Locked while the work ran (power key 12 s in) | The stopwatch read 3 s before the lock and 15 s and 18 s after it, and paused on a tap after it. 794 frames in 25 s. |
| Idle past the screen timeout (timeout set to 15 s, no input for 35 s), phone locked before the display opened | The display's power group stayed awake (`wakefulness 1`, display 0 dozing), the stopwatch read 38 s, a tap still paused it, 1747 frames in 44 s (40 fps). |
| Idle while the phone locks with the display open (later, from main's report) | **Broke without a fix:** about 10 s after the last input, reads of the display turned empty (no app, no elements, 3 ms) and the app slept. With the keep-awake key, reads 8 s and 14 s into a pause and 35 s after the lock still showed the app; through the gateway, `TestEmulatorBackground` passes, and fails at its 8 s read without the key. |
| Typing (`TestDeviceBackgroundLocked`) | Settings on the display, search tapped, "pinky" typed with the companion's `set_text` on that display, read back, video flowing; the phone off and locked. |

Why: from Android 13 scrcpy creates its display with `VIRTUAL_DISPLAY_FLAG_ALWAYS_UNLOCKED` and `OWN_DISPLAY_GROUP`, so the keyguard does not cover it and the display has its own power state. `dumpsys power` shows a second power group that stays awake while group 0 dozes, and `dumpsys display` shows display 0 OFF and the virtual display ON.

**The lock screen's timeout applies to every display group.** While the keyguard shows and has focus, Android sets a 10 s user-activity timeout for all groups (`PowerManagerService: Going to sleep due to timeout ... activityTimeoutWM=10000`; `PowerGroup: Powering off display group due to timeout (groupId= 1, ... millisSinceLastUserActivity=10003)`). The background display's group then dozes, its activities go to sleep (`isSleeping=true`, `visibleRequested=false`, the app at `TOP_SLEEPING`), and accessibility still lists the window but cannot fetch its root, so the companion's tree is empty. The phone locked before the display opened kept working because focus was then on the display's app, not on the keyguard. Input on the virtual display counts as activity for its group only, so the driver sends `KEYCODE_UNKNOWN` there every 4 s when nothing else did; apps ignore that key. `KEYCODE_WAKEUP` sent to the virtual display does not help: it wakes the phone's own screen (group 0) and leaves group 1 dozing.

What it costs and means:

- **The phone's own screen stays off** (no burn-in), but the phone as a whole stays awake while the display is open: its CPU, the encoder while content changes (it sends nothing for a still screen), and the keep-awake key every 4 s. Close the display when the task ends; then the phone sleeps as usual.
- **The lock screen does not protect apps from an agent on a background display.** That is the point of the feature, and it moves all of the protection to the gateway's policy.
- **Android 10 to 12: not verified.** scrcpy sets neither flag before Android 13, so the display most likely sleeps with the phone and the keyguard may cover it. The Android 10 emulator cannot show it (its encoder fails, and a virtual display needs one) and the Pixel is offline.
- **The Mi:** a background display with the Calculator worked again (8 x 9 = 72 by taps, main screen untouched, the launcher still in front, the accessibility setting exactly as before). The locked case could not be tried: the Mi keeps its screen on while charging (`stay_on_while_plugged_in=15`, Developer options > Stay awake, not set by Pinky), and the rules forbid pressing its power key or changing settings.

**Recommendation:** make background work the default for Android 13 and later, with the display closed at the end of each task, and keep the main screen for Android 12 and earlier until a phone shows otherwise.

### The lost taps

Reproduced and fixed. `TestDeviceTapsLand` taps the phone app's 5 key in batches and counts the digits that arrive:

| Batch | Before the fix | After |
| --- | --- | --- |
| Paced 300 ms, back to back, video reset every 400 ms, a stream nobody reads, the companion turned off and on every 1.5 s, screenshots and reads meanwhile, the session restarted right before each tap | 0 lost | 0 lost |
| Another caller reopening the stream with other options every 350 ms while taps go on | **9 of 40 lost** (each failed with "session restarted with new options"; a write that beat the close could also vanish without an error) | 0 lost |
| Android 10: a stream request on a phone without video | 1 of 40 lost (the request closed the session before refusing) | 0 lost |

Totals after the fix: 320 taps on Android 16 and 280 on Android 10, none lost. The Mi's two lost taps (7 and = through the app) fit this: a session restart, or a session that died, while the taps were on their way. Which restart happened that night is not known; the fix covers restarts the driver makes, and taps on a session that dies return an error.

### Android 10 (emulator-5562, standing in for the Pixel)

- **scrcpy:** the server runs, but the emulator image's video encoder fails (so does its `screenrecord`), so the driver runs it without video. The real Pixel streamed fine in round one.
- **Companion:** builds from round three install and run (`set_text`, events; Android 10 has no IME action, so Enter goes through scrcpy). Typing ASCII and CJK passes through `set_text`.
- **uiautomator fallback:** used for the PIN pad while the companion was off.
- **Person watcher:** watches the virtio touchscreens (see above); a finger through the emulator console gives one signal, Pinky's taps none.
- **Unlock:** wrong PIN `ErrLocked`, right PIN 2.4 s with the companion.
- **Device tests:** all pass, or skip what needs video (stream, background displays) or a calculator.

### Timings (emulators unless said)

| What | Time |
| --- | --- |
| `ScreenOn` | 10 to 30 ms (Mi 11 to 170 ms) |
| `Wake` from asleep | 0.46 s |
| `Unlock` with the right PIN | 1.5 s with the companion (Android 16), 2.4 s (Android 10), 4.2 to 5.0 s with uiautomator reading the PIN pad |
| `Unlock` with a wrong PIN, until `ErrLocked` | about 7 s (the 4 s wait for the lock screen to go included) |
| `Unlock` by taps on a pad read from its labels, from the screen off | 3.3 to 3.8 s with the companion, 8 to 12.5 s with uiautomator; a wrong PIN, until `ErrLocked`, about 6 s (Android 10, 16 and 17 emulators, up to four running) |
| `ListFiles` (4 entries), push 300 KB, pull it back | 3 to 14 ms, 12 to 25 ms, 4 to 7 ms |
| `CompanionState` | 7 to 33 ms (Mi, read-only: 79 ms) |
| `InstallCompanion`: turn on only; replace a build signed with another key | 0.18 s; 0.39 s (no Play Protect on the emulators) |
| Background display ready | 0.35 to 0.6 s (Mi 1.1 s) |
| Control-only fallback after a failed encoder | one extra session start, about 0.13 s |

## What works where

| | Mi 10 Pro, Android 17 | Emulator, Android 16 | Emulator, Android 10 | Pixel, Android 10 |
| --- | --- | --- | --- | --- |
| Observe, Screenshot, input, launch, settle | Yes (rounds one and two; Calculator tests again today) | Yes | Yes, without video | Partly (round one) |
| Stream | Yes | Yes | No: the image's encoder fails | Yes (round one) |
| Wake, ScreenOn | ScreenOn read-only | Yes | Yes | |
| Unlock | Yes, by the owner from the computer (key events, past the fingerprint screen) | Yes, right and wrong PIN; also by taps on the pad read from its labels (so is an Android 17 emulator) | Yes; also by taps | Not tried |
| Files | Not tried | Yes | Yes | |
| Companion state and install | State only: `outdated` (build 1 vs 69) | Yes, all states | Yes, incl. replacing a build signed with another key | |
| Background display | Yes, Calculator, unlocked | Yes, also off and locked | No (needs video) | |
| Person input | Pinky's input gives no signal (round two) | Finger: one signal; Pinky: none | Same | |
| Pairing | Yes (2026-11-07, owner at the phone): QR code and pairing code on a server with discovery off, found by Pinky's own search; connect by address; reconnect after a drop | Fake servers | | |

## The Mi 10 Pro in round three

- 09:25: read-only. Screen on and unlocked on the launcher, AC powered, `stay_on_while_plugged_in=15` (`mStayOn=true`): its screen never times out while charging. Other tools' files are in `/data/local/tmp` (AndroMeldAgent, frida-server, Mika), so the owner's own tooling may have set it. Reported to the main session at once.
- 09:40: `CompanionState` = `outdated`. A background display with the Calculator, 8 x 9 = 72; main screen untouched.
- 10:35: `TestDeviceCalculator`, `TestDeviceBackground` and `TestDevicePersonQuietWhileInjecting` (Calculator). The first two passed. The third counted **5 person signals** while Pinky gave input (round two: 0), and afterwards the Mi was **locked with its screen off** (`showing=true`, dozing), although Stay awake is on. The input dispatcher's recent queue had key events and two resets of input device 4 at that time. Pinky never sends the power key (no key code for it, no scrcpy option for it), so someone most likely pressed it and touched the screen: the watcher saw them. As the rules ask, the Mi was left as it was and not used again. The accessibility setting was back to the owner's service, and no `getevent`, `app_process` or Pinky file was left on it.

## Emulator state left behind

- Both: PIN `1234` (`locksettings set-pin`), `stay_on_while_plugged_in=0`, the accessibility setting as found (`null`), the companion session's build 73. `emulator-5560` is unlocked on its home screen; the companion session left `emulator-5562` locked and asleep. `screen_off_timeout` is 600000 on `emulator-5560`; on `emulator-5562` it went back to the image's maximum after the restart. Gboard's stylus tutorial was dismissed on `emulator-5560`.
- `emulator-5562` runs with `-gpu swiftshader_indirect` since a restart at 09:11 (it did not help the encoder).
- `/sdcard/Pinky` exists on both and is empty. No Pinky jar is left in `/data/local/tmp`.

## Removed phones (`driver.Forgetter`, 2026-11-08)

When the person removes a phone from Pinky (gateway notes, "Removing a phone"), the provider hears `Forget(id)` and `Disconnect(ctx, id)`, at start `Forget` again for each phone removed earlier, and `Remember(id)` once it is added back (`forget.go`). The driver hides nothing itself; the gateway keeps the phone off its list.

- `Forget` keeps the Wi-Fi reconnector (Config.ReconnectWiFi) away from the phone: it skips the paired phones whose guid, `adb-<serial>-<suffix>`, holds a removed phone's hardware serial, and neither searches for them nor connects them.
- `Disconnect` drops the phone's Wi-Fi connections that were made by address (by the reconnector, by `ConnectWiFi` and pairings, or by `adb connect`), found by their serial, by the guid in an mDNS name, or by the hardware serial in their properties; `adb.Client.Disconnect` sends `host:disconnect:<serial>` and never an empty serial, which would drop every connection. A USB connection stays. A connection the adb server's own discovery made cannot be dropped: platform-tools 37 reads `adb disconnect adb-3e8be48e-kIY2HE._adb-tls-connect._tcp` as an address, adds port 5555 and answers `no such device 'adb-…._tcp:5555'` (checked on this Mac). It stays connected to the server, which reconnects it after a drop anyway, and Pinky does not use it.
- adb's own files stay as they are: the phone is still in `~/.android/adb_known_hosts.pb` and still trusts this computer's key, for the other tools that share the server. Pairing it again in Add phone, or plugging it in, adds it back.

Checked on `pinky-api36` (`emulator-5560`) through an adb server of the test's own on port 5097 (`ADB_MDNS=0 ADB_USB=0 ADB_EMU=0`, so it found nothing by itself), with the emulator connected by address as a phone on Wi-Fi: `TestRemoveEmulatorOverWiFi` (tag `device`) and `TestRemovePhoneOnEmulator` (tag `e2e`) in `cmd/pinky`. Removing it dropped `127.0.0.1:5561`; connected again with `adb -P 5097 connect`, it stayed off the list, after a restart too; `ConnectWiFi` brought it back. Over a TCP connection the emulator is known by its hardware serial (`EMULATOR37X2X13X0`), and on its own transport by its adb serial (`emulator-5560`), so it cannot stand in for a phone removed over Wi-Fi and then plugged in; the gateway's tests cover that.

## Decisions

Rounds one and two (kept):

1. **Direct abstract-socket connections, no `adb forward`.**
2. **A jar per session, deleted by the server.**
3. **Wake with `KEYCODE_WAKEUP`, never POWER.** Round three removed every scrcpy option that could press POWER or keep a screen awake.
4. **`clipboard_autosync=false`.**
5. **Settle on capture cadence, not size,** 400 ms quiet.
6. **Typing:** the companion's `set_text` first; keys when they reach the field, paste otherwise.
7. **Rows merged into one element,** the way TalkBack reads them.
8. **`App` is the read window's package.**
9. **Background displays without system decorations, keyboard kept local.**
10. **Companion settle: video and events both quiet, tracked locally.**
11. **`set_text` falls back only after a refusal.**
12. **Person input from real devices only** (no `uinput`), 300 ms apart.
13. **Notification text stays out of logs.**

Round three:

14. **Read display 0's power state, not the wakefulness:** a background display keeps the phone awake with its own screen off.
15. **Unlock only after seeing the PIN pad.** Typing digits at a lock screen that has not shown the pad could land some of them in it and count as a failed attempt, so the driver reads the tree first, even at 2 s with uiautomator. `wm dismiss-keyguard` brings up the pad on AOSP-style lock screens without a swipe that vendors place differently; the swipe is the fallback.
16. **The PIN goes as key codes through scrcpy.** Not `input text` (the PIN would be in a process list) and not `set_text` (it would pass through the companion app).
17. **Background `Locked` is false from Android 13,** so the gateway does not stop an agent on a background display because the phone is locked.
18. **A control-only session when the encoder fails,** rather than a driver that cannot tap at all on such a phone.
19. **Restarts wait for the gesture in progress** (one gesture at a time per session), instead of a second session for streams with other options, which would double the encoder's work on the phone.
20. **Replacing a build signed with another key is part of Install:** the person asked for the current companion, the old one cannot be updated, and it is Pinky's own app. Its service is turned off before the uninstall, so Android does not rewrite the person's accessibility setting.
21. **`CompanionState` reports `off` while the service is on only for the driver's reads,** so the client's Install card reflects what the person chose, not the driver's last minute.
22. **The bundled APK's version comes from `assets/pinky-companion.json`,** which the companion build writes, rather than parsing the binary manifest.
23. **Pairing finds the phone by its guid,** and connects by hand only when the adb server has not connected by itself after a second.
24. **Multi-touch devices count as touchscreens on a phone without a direct one** (the emulators): otherwise the watcher would see nothing there.
25. **A background display gives the person's tasks back and destroys only the agent's.** Destroying everything (scrcpy's default) lost the person's task when the agent used an app they had open; moving everything back put the agent's last app on top of the person's screen. Given back, the person's task also lands on top, which is the smaller harm.
26. **A no-op key keeps a background display awake** while it is open, rather than a wake lock (which would need a change to the bundled scrcpy server or the companion app) or `KEYCODE_WAKEUP` (which wakes the phone's own screen). It is sent only after 4 s without other input on the display.
27. **The last driver to leave turns the companion off,** so two Pinky processes (or Pinky and `androidctl`) do not cut each other off and the setting still goes back: the app's three-minute backstop did not turn the service off on `emulator-5560` in a four-minute wait with no client: build 73 restarted its count whenever Android bound the service again, which every `uiautomator dump` causes. Build 74 fixed that, but older builds (the Mi's) still have it, so the driver does not rely on the backstop.

Wi-Fi without adb's discovery (2026-11-07):

28. **Pinky searches the network itself instead of restarting the adb server.** A restart fixes the server's discovery but drops every other tool's connections and the emulators', and the next network change breaks it again. The server still pairs and connects; only finding the phone moved to the driver.
29. **Questions with the unicast-response bit from a port of Pinky's own, answers also read on a socket bound to the group address.** The Mi answers such questions straight back and multicasts answers to plain ones; binding the wildcard address on 5353, as most libraries do, would take the unicast answers meant for mDNSResponder. Pinky never answers or announces anything. Follow-up questions for a missing SRV or address record come at most once a second.
30. **Reconnect what adb would:** the phones in its known hosts, when their connect service is on the network and the server does not list it. A phone the server lists is left to the server.
31. **A search the system blocks is told by its own questions not coming back** (multicast loopback to the group socket), since macOS drops a blocked app's traffic without an error.

Pinky's own adb (2026-11-08):

32. **Built from AOSP's source rather than Google's platform-tools,** which come under Google's SDK terms, which forbid passing them on. android-tools' CMake build, cut down to adb, is the shortest way there; the Windows adb is 32-bit like Google's because AOSP's AdbWinApi.dll, which Samsung's and the other makers' older drivers need, is.
33. **The person's adb before Pinky's own,** so that their tools and Pinky share one server of one version and neither kills the other's.
34. **The server process started directly** (`fork-server server`), never through `adb start-server`, which kills a server of another version.
35. **Pinky's own adb from a copy in the cache,** not from the app's folder, so that updates and disk images are never held by the server, which outlives Pinky.

## Not done, and open questions

- **Android 10 to 12 with a background display while locked:** expected not to work (see above); needs a real phone.
- **A wake lock instead of the keep-awake key:** the companion session could add a `keep_awake` command that puts a 1-pixel, untouchable accessibility overlay with `FLAG_KEEP_SCREEN_ON` on a given display, which recent Android versions turn into a wake lock for that display. Not built or tested; the key works.
- **The app's own Local Network prompt** was not seen: the verification ran through `androidctl`. A Pinky.app without Local Network access shows "Pinky isn't allowed to look for phones on this network" in Add phone after 30 s.
- **The Mi:** locked since about 10:36, probably by someone at it; Stay awake keeps its screen on while charging, which the owner may want off. It still has companion build 1; `InstallCompanion` there will replace it and Play Protect will ask.
- **The Pixel:** still offline.
- **Screenshot on a virtual display** returns `ErrUnsupported` (`screencap` rejects virtual display ids); decode a frame from the display's stream.
- **Screens that never stop moving** reach the settle limit; the phone app's favorites page on the Android 16 emulator is one.
- **uiautomator unbinds every accessibility service** while it dumps; with the companion on, the driver never runs it.
