<p align="center"><img src="assets/icon/pinky-macos-1024.png" width="112" alt=""></p>
<h1 align="center">Pinky</h1>
<p align="center"><b>Let any AI agent use your iPhone or Android phone, from macOS, Windows or Linux, with you in the loop.</b></p>
<p align="center"><a href="https://github.com/DIYgod/pinky/releases/latest">Download</a> · <a href="#get-started">Get started</a> · <a href="#faq">FAQ</a> · <a href="https://discord.gg/S27b7SHjng">Discord</a></p>
<p align="center">English · <a href="README.zh-CN.md">简体中文</a></p>
<p align="center"><img src="assets/readme/demo.gif" width="760" alt="Claude Code types a reply in Messages; Pinky holds the Send tap until it is allowed, then the text goes out"><br><sub>A real Claude Code run on an Android 16 emulator. Pinky asks before payments by default; this run also asks before messages.</sub></p>

Pinky lets any agent (Claude Code, Claude Desktop, Codex, Cursor or a script) operate a real iPhone or Android phone over MCP, while you watch on your computer, approve risky steps, take over and review what happened. Approvals are enforced by Pinky's gateway, not by instructions to the agent.

- **Every platform.** One app for macOS (Apple silicon and Intel), Windows and Linux. It drives Android phones (Android 10 to 17) and iPhones on iOS 17.4 or later: nothing to install on iOS 27 and later, and a helper installed once from Xcode on iOS 17.4 to 26, which you can also choose on iOS 27 (tried there and on Simulators, not yet on a phone below 27; [details](#phones)).
- **Tiny and native.** The window is drawn by [MyGo](https://github.com/egoist/mygo) on the GPU, with no Electron and no webview: a 16–22 MB download that idles at about 70 MB of memory and 0% CPU on a Mac.
- **Any agent, one click.** Settings › Agents connects Claude Code, Claude Desktop, Codex, Cursor and more, and any agent that can use MCP servers works, on the same computer or over HTTP.
- **Locked phone? Unlock it from your computer.** Save the phone's PIN in Pinky once, then confirm each unlock with Touch ID, Windows Hello or your keyring's password. Agents never see the PIN.
- **Free and open source** under the Apache-2.0 license, with no account to create.

<table>
<tr>
<td width="50%" valign="top"><img src="assets/readme/card-approval.jpg" width="100%" alt="You decide what waits: Pinky's window holds an agent's Send tap until you click Allow"><br><b>You approve the risky steps.</b> Payments, trades and transfers wait for your click; in Settings › Safety you can have sends, orders, deletions, calls, installs, links that leave the app and settings changes wait too, or every step. Pinky spots these buttons in 10 languages. The rules run in Pinky's gateway, so an agent can't approve itself, even when text on the screen tries to talk it into it.</td>
<td width="50%" valign="top"><img src="assets/readme/card-off-limits.jpg" width="100%" alt="Off-limits by default: Settings › Safety with when agents ask you, and the blocked apps"><br><b>Off-limits by default.</b> Banking, wallet and password-manager apps are invisible to agents, and agents can't type into password or one-time-code fields. When an agent needs a banking or wallet app for a task, it asks, and you can allow that app for that task only. Every new agent needs your approval and gets only the scope you give it.</td>
</tr>
<tr>
<td width="50%" valign="top"><img src="assets/readme/card-second-screen.jpg" width="100%" alt="A hidden second screen for the agent: the agent's screen beside the phone's own home screen"><br><b>Your phone stays yours.</b> On Android 13 and later, agents work on a hidden second screen, so the phone's own screen stays yours, or off and locked. On older Android phones and on iPhones they use the main screen.</td>
<td width="50%" valign="top"><img src="assets/readme/card-take-over.jpg" width="100%" alt="Take over any time: Claude Code is paused while you are in control, until you hand back"><br><b>Take over any time.</b> Click the mirror and the agent pauses until you hand back; on Android, touching the phone yourself pauses it too. Stop all is ⌥⌘. (Ctrl+Alt+. on Windows and Linux) from anywhere.</td>
</tr>
<tr>
<td width="50%" valign="top"><img src="assets/readme/card-recording.jpg" width="100%" alt="Every run, on the record: a finished task's steps beside its screen recording"><br><b>Every run is on the record.</b> Each step in plain words, screenshots and a screen recording with a mark per step, exportable as an HTML report. It all stays on your computer.</td>
<td width="50%" valign="top"><img src="assets/readme/card-iphone.jpg" width="100%" alt="On iPhone too: Pinky holds a Delete tap in Photos until you click Allow"><br><b>iPhones too.</b> The same gateway, approvals and records work on iOS 17.4 and later (<a href="#phones">what works</a>). Shown here on the iOS Simulator, with deletions set to ask.</td>
</tr>
</table>

**Status:** pre-alpha. Claude Code has used real phones through it end to end (an Android phone over USB and an iPhone on iOS 27), and agents can connect over stdio or HTTP. Unlocking from the computer, background work on Android, recordings, export, Wi-Fi pairing and the phone's files are built; some of it still waits for a person to try it. See [docs/ACCEPTANCE.md](docs/ACCEPTANCE.md) for what works today and how to try it.

## Get started

Install Pinky, connect your phone and add Pinky to your agent. Then ask the agent to do something on the phone, and watch it work in Pinky's window.

### What you need

| | You need |
| --- | --- |
| **Mac** | macOS 12 or later, with Apple silicon or Intel |
| **Windows** | Windows 10 or 11, 64-bit (x64) |
| **Linux** | A 64-bit PC (x86-64) with Ubuntu 20.04, Debian 11 or a newer system (glibc 2.29 or later), and GTK 3 |
| **Android phone** | Android 10 to 17 and a USB cable. Pinky brings its own adb; on Windows some phones need a USB driver, and on Linux USB access ([below](#usb-on-windows-and-linux)) |
| **iPhone** | iOS 17.4 or later and a USB cable. Pinky prepares it with Apple's developer disk image, which comes with Xcode or which Pinky can download from a third-party mirror when you ask. On iOS 27 Pinky can't mount the image itself yet: open Xcode once with the iPhone plugged in, and the image stays ([below](#iphone)). On iOS 17.4 to 26 also a helper app on the iPhone ([below](#iphones-on-ios-174-to-26)) |
| **Agent** | Any agent that can use MCP servers, such as Claude Code, Claude Desktop, Codex or Cursor |

### 1. Download and install Pinky

Get Pinky from [the latest release](https://github.com/DIYgod/pinky/releases/latest):

- **Mac:** download `pinky-<version>-arm64.dmg` for a Mac with Apple silicon (M1 or later) or `pinky-<version>-amd64.dmg` for an Intel Mac. Open it and drag Pinky to Applications. With [Homebrew](https://brew.sh), `brew install --cask diygod/tap/pinky` does the same.
- **Windows:** download `pinky-setup-<version>-amd64.exe` and run it. It installs Pinky for your account, without administrator rights. The installer is not signed yet, so SmartScreen may say that Windows protected your PC: choose **More info**, then **Run anyway**. To use Pinky without installing it, unzip `pinky-<version>-windows-amd64.zip` and open `Pinky.exe`.
- **Linux:** on Debian and Ubuntu, download `pinky_<version>_amd64.deb` and install it with `sudo apt install ./pinky_<version>_amd64.deb`. On other systems, unpack `pinky-<version>-linux-amd64.tar.gz` into a folder of its own and run `pinky` in it.

Pinky looks for new versions by itself and, on a Mac and with the Windows installer, installs them too ([Updates](#updates)).

### 2. Connect your phone

Open Pinky and choose **Add phone**. A phone that is ready shows up in Pinky's window; when something is missing, the phone's page in Pinky says what.

#### Android

**Add phone › Android** shows these steps for your phone's brand, with its own menu names and extra switches: Xiaomi, OPPO, vivo, HONOR, HUAWEI, Samsung and Pixel, and general steps for the rest.

1. On the phone, turn on USB debugging: in Settings › About phone, tap **Build number** 7 times, then turn on **USB debugging** in Settings › System › Developer options. Some brands name these menus a little differently.
2. Plug the phone into the computer. When the phone asks whether to allow USB debugging, tap **Allow**. On Android 11 and later you can pair it over Wi-Fi instead, from **Add phone**.
3. Pinky offers to install Pinky Companion, its app for the phone, which makes reading the screen many times faster and lets Pinky type in any language. The phone may ask you to allow the install once.

Each phone has a **Checkup**, in the ··· menu under its mirror, that lists what works and how to fix what doesn't, such as a Xiaomi's USB debugging (Security settings) switch.

<table>
<tr>
<td width="50%" valign="top"><img src="assets/readme/add-android.png" width="100%" alt="Add phone › Android with a Xiaomi picked: where to turn on Developer options, and the three USB debugging switches"><br><sub><b>Add phone › Android</b>, with a Xiaomi picked.</sub></td>
<td width="50%" valign="top"><img src="assets/readme/checkup.png" width="100%" alt="A Redmi's checkup: adb, the connection and debugging pass; Pinky Companion and tapping need fixing, with the steps"><br><sub>A Redmi's <b>Checkup</b>, with two things to fix.</sub></td>
</tr>
</table>

#### Android tools

Pinky brings its own adb, the tool it reaches Android phones with, built from Android's source, so nothing else needs installing. When the computer has Android's platform tools already, from Android Studio, Homebrew or the system's packages, Pinky uses those, and shares their adb server with Android Studio and other tools.

#### USB on Windows and Linux

- **Windows:** the phone's USB debugging needs a driver. Windows installs one by itself for many phones; when it has none, Pinky lists the phone as needing setup and says which driver to install: the [Google USB Driver](https://developer.android.com/studio/run/win-usb) for Pixels, [Samsung's](https://developer.samsung.com/android-usb-driver) for Samsung phones, the [phone maker's](https://developer.android.com/studio/run/oem-usb) for others. Unplug the phone and plug it in again afterwards.
- **Linux:** the computer must let Pinky open the phone's USB device, which takes udev rules for Android phones: `sudo apt install android-sdk-platform-tools-common` on Debian and Ubuntu, `android-udev` on Arch, `android-tools` on Fedora. You may also need to be in the `plugdev` group. Then plug the phone in again. Pinky says so when they are missing.

#### iPhone

Pinky prepares an iPhone itself, except for one step on iOS 27 for now (below); **Add phone › iPhone** walks you through it.

1. Plug the iPhone into the computer, unlock it, tap **Trust** and enter its passcode. On Windows, install Apple Devices from the Microsoft Store (or iTunes) first; on Linux, the `usbmuxd` package. Add phone says when they are missing.
2. On the iPhone, turn on **Developer Mode** in Settings › Privacy & Security: Pinky makes the switch appear there. The iPhone restarts; unlock it, tap **Turn On** and enter the passcode.
3. Pinky mounts Apple's developer disk image on the iPhone and lists it as ready. An iPhone on iOS 27 keeps the image when it restarts; below iOS 27 Pinky mounts it again after each restart, once you unlock the iPhone.

The developer disk image comes with [Xcode](https://apps.apple.com/app/xcode/id497799835), Apple's free app for developers, and Pinky does not ship it. On a Mac, install Xcode once and open it: Pinky uses the image it installs, and Xcode never has to run again. On Windows and Linux, copy `/Library/Developer/DeveloperDiskImages/iOS_DDI` from such a Mac into Pinky's folder ([where](docs/notes/ios.md#preparing-a-phone-without-xcode)). Without either, Add phone offers to download the image from a third-party mirror, github.com/doronz88/DeveloperDiskImage (Xcode 27.0 beta 4, 32 MB, kept by the author of pymobiledevice3, not by Apple), only when you click Download; Pinky checks every file against the checksum it expects. On iOS 27 and later, no app gets installed on the iPhone.

Checked on the iPhone 13 mini (iOS 27.0.1): finding Xcode's image, keeping the image already on the phone, and that the iPhone keeps the image when it restarts. Pinky mounting the image itself does not work there yet: the install waits until it times out. Until it does, mount the image once by opening Xcode with the iPhone plugged in; it then stays. Not yet tried on a phone: the downloaded image, the Developer Mode switch, iPhones below iOS 27, and iPhones on Windows or Linux.

#### iPhones on iOS 17.4 to 26

Below iOS 27, an iPhone takes taps and typing from a computer only through a helper app on the phone: WebDriverAgent, which you build and install once from Xcode with your Apple ID ([how](docs/notes/ios.md#installing-the-webdriveragent-helper)). With a free Apple ID it has to be installed again every 7 days. It has not been tried on an iPhone below iOS 27 yet. Without it, Pinky can only show and read the screen (on a Mac) and open apps: no taps, swipes or typing. iOS 17.3 and earlier are not supported.

### 3. Add Pinky to your agent

In Pinky, open **Settings › Agents** and click **Connect** next to your agent. Pinky finds Claude Code, Claude Desktop, Codex, Cursor, VS Code, Copilot CLI, Windsurf (Devin Desktop) and Gemini CLI on your computer and adds itself to the agent's MCP servers, by the app's own path: with the agent's own command where it has one, such as `claude mcp add`, or in its settings file, which Pinky copies first and changes only in Pinky's entry. It also installs Pinky's agent guide, [SKILL.md](docs/SKILL.md), where the agent reads skills. Until an agent is connected, a card in Pinky's sidebar offers to do it, or says where to get one.

<p align="center"><img src="assets/readme/agents.png" width="620" alt="Settings › Agents › Connect an agent: Claude Code connected; Claude Desktop, Codex, Cursor and VS Code with Connect buttons"></p>

- **Claude Desktop:** Connect opens Pinky's extension in Claude Desktop; click **Install** there. Double-clicking `pinky-<version>.mcpb` from the [release page](https://github.com/DIYgod/pinky/releases/latest) does the same.
- **Other agents:** **Settings › Agents › Add an agent by hand** shows the command for your copy of Pinky, with buttons that copy it. Add an MCP server (stdio) that runs it. In a JSON config, with the Homebrew cask or a Linux package:

  ```json
  { "mcpServers": { "pinky": { "command": "pinkyctl", "args": ["mcp"] } } }
  ```

  Claude Code in a terminal: `claude mcp add --scope user pinky -- pinkyctl mcp`. Most agents read the guide from `~/.agents/skills/pinky/SKILL.md`, Claude Code from `~/.claude/skills/pinky/SKILL.md`.

The command is `pinkyctl`, not `pinky`: many computers already have another program named `pinky`. An agent on another computer or in a container can connect over HTTP instead ([how](docs/mcp.md#over-http)). [docs/mcp.md](docs/mcp.md#connecting-an-agent) says what Connect changes for each agent.

### 4. Try it

Ask the agent to do something on your phone, for example "Open Messages on my phone and tell Sam I'm running late." The first time an agent connects, Pinky asks whether to allow it and what it may do. Payments wait for your click in Pinky's window; in Settings › Safety you can have sends, deletions and more wait too.

No phone at hand? Quit Pinky and start it with two scripted demo phones (a payment to approve, a blocked banking app, a password field): `/Applications/Pinky.app/Contents/MacOS/Pinky --fake` on a Mac, `pinkyctl --fake` on Linux, or `& "$env:LOCALAPPDATA\Programs\Pinky\Pinky.exe" --fake` in PowerShell on Windows.

## Using Pinky

### Phones

- **Android:** connected over USB or Wi-Fi ([above](#android)). Pinky works over adb and a bundled scrcpy server. Its companion app, which ships with Pinky, reads the screen in about 70 ms instead of 2 s and types any language without the clipboard: the phone's page offers to install it (the phone may ask you to allow it once). Its accessibility service is on only while an agent works on the phone, as banking apps refuse to run next to one ([why](#faq)). On Android 13 and later, agents work on a background display, so the phone's own screen can stay off and locked.
- **iPhone:** connected over USB and prepared by Pinky ([above](#iphone)). What Pinky can do depends on the iOS version and on how Pinky connects, which you can choose for each iPhone in Settings › Phones (Automatic picks by version, as below):

  | iOS | How | What works | Tested on |
  | --- | --- | --- | --- |
  | 27 and later | Xcode's Device Hub protocol; nothing installed on the phone | Everything. Typing pastes the text and puts your clipboard back | iPhone 13 mini, iOS 27.0.1 |
  | 17.4 and later, with the helper | The WebDriverAgent helper, installed once from Xcode ([how](docs/notes/ios.md#installing-the-webdriveragent-helper)): automatic on iOS 17.4 to 26, your choice on 27 | Everything, and the phone shares no screen, so banking apps that refuse screen sharing keep working (DBS digibank and China Merchants Bank in testing). Slower than Device Hub: a home screen reads in about 7 s instead of under 0.5 s, a tap takes 0.5 to 0.8 s. Now and then iOS asks for the passcode on the phone before the helper starts. The live picture needs macOS | iPhone 13 mini, iOS 27.0.1, with the helper chosen; iOS 18.5 and 26.5 Simulators. Not yet on a phone below iOS 27 |
  | 17.4 to 26, without it | The developer services alone; nothing installed | Watching (a few frames a second, macOS only), reading the screen (placed by OCR, macOS only) and opening apps. No tapping, swiping, typing or Home: the phone offers no input without the helper | The same services on iOS 27.0.1; not yet on a phone below iOS 27 |
  | 17.3 and earlier | | Not supported: before 17.4 the connection needs administrator rights, and iOS 16 and earlier need a developer disk image that current Xcode no longer includes | |

  The helper is Appium's WebDriverAgent, signed with your own Apple ID; with a free Apple ID it has to be installed again every 7 days.
- **Screen sharing on iPhone:** while Pinky streams an iPhone's screen, iOS tells apps that the screen is being shared, and some banking apps (DBS digibank and China Merchants Bank among them) refuse to run or warn you. Pinky streams it while the phone's page or tile is open in Pinky, while an agent works on it, and for 30 seconds after; then such apps work again. Over CoreDevice, agents cannot use them. Connect that iPhone through the WebDriverAgent helper instead (Settings › Phones): the phone then shares no screen, and agents can use those apps too.
- **Locked phones:** Pinky never keeps a phone awake. It wakes the screen before a step; save the phone's PIN in Settings › Phones to unlock it from the computer with Touch ID. Agents never see the PIN. On Android this is checked on AOSP-style lock screens (LineageOS on a Mi 10 Pro, the Android 10, 16 and 17 emulators); the lock screens of Xiaomi, Samsung, OPPO, OnePlus, vivo, Honor and Huawei are only covered by tests made from what is known of them, and where Pinky cannot read the PIN pad with confidence it enters nothing ([details](docs/notes/integration.md#unlocking-other-android-phones)).
- **Removing a phone:** **Remove this phone…** in the phone's ⋯ menu (also on its page while it is offline, on its tile in All phones, in its right-click menu in the sidebar and in Settings › Phones) takes it off the list and deletes its saved PIN and its settings. Its tasks, screenshots and recordings stay in Activity unless you tick the box to delete them too, and a task running on it stops. A phone on a cable comes back at once, as a new phone, until you unplug it. A phone on Wi-Fi is disconnected and stays off the list, even when adb connects it again by itself; to add it back, pair it again in Add phone or plug it in. Pinky does not unpair the phone from your computer: its USB or wireless debugging pairing, or the iPhone's trust, stays for other apps ([details](docs/notes/gateway.md#removing-a-phone)).

### Updates

Pinky looks for a new version at launch and every few hours, and shows a card with what changed, **Update now** and **Later**. With Settings › Advanced › **Update automatically** (on by default) it installs it by itself once no agent is working and nobody has used Pinky for ten minutes, and it updates Pinky Companion on Android phones that are free; its accessibility service stays off until the next task. A Pinky Companion signed with the public key that its builds had until 8 November 2026 is uninstalled and installed again, signed with Pinky's private key, and Pinky tells you. After an update, **What's new** lists the changes since the version you had (from [CHANGELOG.md](CHANGELOG.md)).

- **macOS:** Pinky.app replaces itself after checking the download's checksum, that it is signed by Pinky's developer and that Apple notarized it, and goes back to the old version if the new one does not start. The Homebrew cask's copy updates itself the same way, and `brew upgrade` leaves it alone while it is current. A copy run from the disk image or from a folder you cannot write to says how to update it instead.
- **Windows:** a Pinky that its installer installed runs the new installer quietly, after checking its checksum, starts the new version and goes back to the old one if that does not start. The portable copy downloads the installer for you.
- **Linux:** Pinky downloads the release for you to install.
- **Terminal:** `pinkyctl update` installs an update while Pinky is not running; `pinkyctl daemon` only logs that one is out.

Details: [docs/notes/updates.md](docs/notes/updates.md).

### Usage statistics, crash reports and feedback

Pinky shares anonymous usage statistics (counters such as tasks run, tools called and phones connected, about once a day, with a random install id) and crash reports; turn both off in Settings › Privacy or with `DO_NOT_TRACK=1`. Screens, typed text, app names, phone names and ids, PINs and file names are never sent. Help › Send Feedback… opens a GitHub issue with your message and diagnostics filled in, which you read and submit yourself. [docs/TELEMETRY.md](docs/TELEMETRY.md) lists everything.

## FAQ

<details>
<summary><b>Which agents work with Pinky?</b></summary>

Any agent that can use MCP servers: over stdio, by running `pinkyctl mcp`, or over HTTP with an agent token ([how](docs/mcp.md#over-http)). **Settings › Agents** connects Claude Code, Claude Desktop, Codex, Cursor and other agents it finds on your computer in one click. Claude Code is the one that has used real phones end to end so far. Pinky brings no model of its own: your agent does the thinking.

</details>

<details>
<summary><b>Can an agent approve its own risky step, or be talked into it?</b></summary>

No. The rules run in Pinky's gateway, between the agent and the phone. A step that needs you comes back to the agent as `approval_pending`, and nothing happens on the phone until you click **Allow** in Pinky; the agent has nothing it could send to confirm it. Before acting, Pinky checks that the button is still where it was, and no answer within 60 seconds means no. Pinky finds these buttons by their labels in 10 languages, so an unusual one could slip through: for more control, Settings › Safety has **Ask before every action** and **Read-only mode**.

</details>

<details>
<summary><b>What can't agents see?</b></summary>

Banking, wallet and password-manager apps, from a list that comes with Pinky and that you can add to in Settings › Safety: agents get no screen from them, and their notifications are hidden. Agents can't type into password or one-time-code fields, and codes in notifications are masked. When an agent needs a banking or wallet app for a task, Pinky asks you first. If you allow it, the agent may open and read that app for the rest of the task, what your rules ask about there still asks, and Pinky keeps no screenshots or recordings of it. Password managers are never offered.

</details>

<details>
<summary><b>Does my phone's screen go to the cloud?</b></summary>

Not through Pinky. Screenshots, recordings and history stay on your computer, for 30 days unless you change it in Settings › Privacy, where you can also delete them. What the agent reads goes to the agent you chose, as with any tool you give it. Pinky's anonymous usage statistics never include screens, typed text or app names ([above](#usage-statistics-crash-reports-and-feedback)).

</details>

<details>
<summary><b>What does Pinky install on my phone?</b></summary>

On Android, Pinky Companion, when you click Install; Pinky keeps it up to date and turns its accessibility service on only while an agent works ([why](#faq)). On an iPhone, no app on iOS 27 and later: Pinky mounts Apple's developer disk image, as Xcode does (on iOS 27, for now, Xcode has to mount it once; [details](#iphone)). On iOS 17.4 to 26, the WebDriverAgent helper, which you install once from Xcode ([how](docs/notes/ios.md#installing-the-webdriveragent-helper)).

</details>

<details>
<summary><b>Will banking or other apps notice Pinky Companion?</b></summary>

While its accessibility service is on, they can. Malware abuses accessibility services, so many banking apps refuse to sign in while an app from outside an app store has one turned on: in Malaysia (Maybank, CIMB, HSBC and the other members of the Association of Banks in Malaysia, since 2024) and in Singapore (DBS, OCBC and UOB, since 2023) among others. Pinky Companion is such an app, as Pinky installs it over USB. Apps such as WeChat, Douyin and Xiaohongshu can see the service too and watch for automated use; we know of no report that they flagged Pinky Companion, but letting an agent use them may be against their terms.

So Pinky turns the service on only while an agent works on the phone: as a task starts, and off again a few seconds after the task ends or you take over the phone, or a minute after the agent's last step. The rest of the time it is off, and those apps work as usual. While an agent works, even on the phone's background display, they see it: to use one of them then, click **Take over** in Pinky first, or end the task.

To keep Pinky from turning the service on at all, uninstall Pinky Companion on the phone (Android's Settings › Apps). Pinky works without it, only more slowly: it reads a screen in about 2 s instead of 70 ms, and types text other than plain English through the phone's clipboard. **Not now** on the phone's page puts away Pinky's offer to install it again.

</details>

<details>
<summary><b>How is Pinky different from other phone tools for agents?</b></summary>

We looked at more than a dozen before building it. None of the open-source ones covers iPhone and Android, runs on macOS, Windows and Linux, works with any agent and keeps a person in the loop, all at once. Most leave it to the agent to ask first, or block whole kinds of actions; Pinky holds the one risky tap itself, outside the agent, and records every run.

</details>

<details>
<summary><b>Is it safe to give my phone to an agent that can run commands?</b></summary>

An agent that can run commands as you could reach the phone without Pinky, through adb for example, and skip its rules. Run such agents in a sandbox without access to the phone, or plug the phone into another computer and let the agent connect to Pinky there over HTTP ([how](docs/mcp.md#over-http)).

</details>

## Build from source

To use Pinky, install a release ([Get started](#get-started)). Building it yourself, to work on it, needs Go 1.27.3 or later, and on a Mac Xcode's command line tools.

Pinky is one Go binary without cgo; MyGo (`go tool mygo build`) packages it for each system. The live mirror and recordings use FFmpeg 9's shared libraries, which Pinky loads at run time: from `PINKY_FFMPEG_DIR`, from the app (macOS), next to the executable, then from where the system keeps libraries. Every release carries its own minimal LGPL build (`scripts/ffmpeg.sh`, the H.264 and HEVC decoders): in Pinky.app, and next to the executable in the Windows installer and zip and in the Linux `.tar.gz` and `.deb` ([docs/RELEASING.md](docs/RELEASING.md#ffmpeg)). A build of your own needs one too: `make app` adds it to Pinky.app, and `scripts/release.sh build` makes every package with it. Without FFmpeg everything but the mirror works, and Pinky says what is missing.

### macOS

```sh
make companion   # optional: the Android companion APK (Android SDK and JDK)
make app         # build/pinky/darwin-<arch>/Pinky.app and pinky-<version>-<arch>.dmg
```

`make app` builds a minimal LGPL FFmpeg 9 from source the first time (`make ffmpeg`, about a minute: the H.264 and HEVC decoders with VideoToolbox), puts it in `Pinky.app/Contents/Frameworks`, with CMake installed (`brew install cmake`) also builds Pinky's own adb (`make adb`) into `Contents/Helpers`, adds the companion APK when it is built (`COMPANION_APK=path` picks another), signs the app and makes the `.dmg` from the finished app. Install from the `.dmg` by dragging Pinky to Applications. The app is signed ad hoc unless `macos.signingIdentity` in `cmd/pinky/mygo.json` (or `make app MACOS_SIGN=…`) names a Developer ID; an ad-hoc signed app runs on the Mac that built it, and another Mac's Gatekeeper refuses it. `make app` does not notarize. `make release` signs with the Developer ID in `RELEASE_SIGN` and notarizes the `.dmg` (`make notarize`: submit, wait, staple), with the notarytool profile in `NOTARY_PROFILE` (`pinky-notary`), which you store once with `xcrun notarytool store-credentials pinky-notary --apple-id <Apple ID> --team-id <team>`. Releases themselves are built, signed and published by a workflow for both kinds of Mac, Windows and Linux; see [docs/RELEASING.md](docs/RELEASING.md).

To try your build, open it (`open build/pinky/darwin-arm64/Pinky.app`) and connect your agent to it with Connect in its Settings › Agents, which uses this build's path, or for Claude Code from the checkout:

```sh
claude mcp add --scope user pinky -- "$PWD/build/pinky/darwin-arm64/Pinky.app/Contents/MacOS/Pinky" mcp
mkdir -p ~/.claude/skills/pinky && cp docs/SKILL.md ~/.claude/skills/pinky/SKILL.md
```

`go run ./cmd/pinky` does not see the app's FFmpeg: install FFmpeg 9 with Homebrew (`brew install ffmpeg`) or point at the build, `PINKY_FFMPEG_DIR=build/ffmpeg/lib go run ./cmd/pinky`.

Unlocking a phone from the computer keeps its PIN in the login keychain and asks for Touch ID or the Mac's password each time.

### Windows

```sh
go tool mygo build -platform windows/amd64 cmd/pinky   # build/pinky/windows-amd64/Pinky.exe (and an installer where NSIS is installed)
go build -o pinky.exe ./cmd/pinky                      # a console build, for pinky devices and other commands in a terminal
```

Pinky.exe is a windowed program: agents that start `Pinky.exe mcp` talk to it over pipes, but a terminal shows nothing it prints, so use the console build there. A release's installer and zip carry FFmpeg next to `Pinky.exe` (with D3D11VA). For a build of your own, `scripts/ffmpeg.sh build/ffmpeg-windows windows-amd64` makes the DLLs in `build/ffmpeg-windows/bin` (in a Docker container); put them next to the executable, or set `PINKY_FFMPEG_DIR` to their folder (the `ffmpeg-n9.0-…-win64-lgpl-shared` builds at [BtbN/FFmpeg-Builds](https://github.com/BtbN/FFmpeg-Builds/releases) work too). A build of your own carries no adb, so Android phones need the one of Android's platform tools; `scripts/adb.sh build/adb-windows windows-amd64` builds Pinky's own (in a Docker container), whose `adb.exe` and two DLLs go next to the executable. iPhones are untested on Windows. PINs are kept in the Credential Manager, and each unlock asks through Windows Hello, or for the account's password where Hello is not set up.

### Linux

```sh
go tool mygo build -platform linux/amd64 cmd/pinky   # build/pinky/linux-amd64/: pinky, a .deb, a .tar.gz with install.sh, a PKGBUILD
```

The window needs GTK 3 and EGL (`libgtk-3-0`, `libegl1`; Mesa draws it in software where there is no GPU). A release's `.tar.gz` and `.deb` carry FFmpeg next to `pinky`; it needs glibc 2.29 or later (Ubuntu 20.04, Debian 11 and newer) and decodes in software. For a build of your own, `scripts/ffmpeg.sh build/ffmpeg-linux linux-amd64` makes the libraries in `build/ffmpeg-linux/lib` (in a Docker container); put them next to the executable, or set `PINKY_FFMPEG_DIR` to their folder. For hardware decoding (VA-API), point `PINKY_FFMPEG_DIR` at an FFmpeg 9 that has it, such as the distribution's or BtbN's shared builds (which need `libva2` and `libva-drm2`); Pinky looks there first. iPhones are untested on Linux. PINs are kept in the Secret Service (GNOME Keyring, KWallet, KeePassXC) in a keyring of Pinky's own, which the first save asks you to give a password and which Pinky locks again after each use, so every unlock asks for that password.

## Build and test

```sh
make check          # build (CGO_ENABLED=0), vet, test
make app            # Pinky.app with FFmpeg and the companion, and a .dmg (macOS)
make ffmpeg         # FFmpeg 9 for Pinky.app, in build/ffmpeg
make adb            # Pinky's own adb for Pinky.app, in build/adb (needs CMake)
make companion      # the Android companion APK (Android SDK and JDK needed)
make e2e            # a binary with MyGo's automation socket, for UI checks
scripts/smoke.sh    # the fake gateway, pinky devices and a task through pinky mcp
make dist           # a dry run of a release, in dist/<version>/ (docs/RELEASING.md)
```

`go run ./cmd/pinky --fake` runs the app with two scripted phones (a payment to approve, a blocked banking app, a password field), so it needs no phone.

Tests that need a real phone use the `device` build tag; see [docs/WORKING.md](docs/WORKING.md). `go run ./scripts/secretcheck` checks by hand that unlocking asks you (Touch ID, Windows Hello). `PINKY_FFMPEG_APP=build/pinky/darwin-arm64/Pinky.app go test -run TestPackagedFFmpeg ./internal/video/` checks that the app decodes with its own FFmpeg; given the folder of an unpacked Windows or Linux package, it checks that package's.

CI ([.github/workflows/ci.yml](.github/workflows/ci.yml)) builds, vets and tests on Linux and Windows, builds and vets on macOS, runs `scripts/smoke.sh` on all three, packages for Linux and Windows, and on Linux opens the window under Xvfb and captures it through the automation socket. It runs for `main`, pull requests, by hand, and the platform session's `claude/platform-*` branches.

## Code layout

```text
cmd/pinky/                    app, daemon and MCP shim in one binary
internal/screen/              the screen format agents read
internal/driver/              driver interfaces; android/ and ios/ implement them
internal/gateway/             sessions, leases, the action pipeline, the client API
internal/policy/, approval/   rules and approvals
internal/history/             task history (SQLite)
internal/mcp/                 the MCP server
internal/api/                 the local socket for agents and clients
internal/demo/                scripted fake phones
internal/update/              new versions: the feed, checks, the swap, What's new
internal/video/, ui/          the desktop client (MyGo native UI, FFmpeg decoding)
internal/telemetry/           usage statistics, crash reports and feedback
worker/                       the Cloudflare Worker that receives usage statistics and crash reports
android/companion/            the Android companion app (Kotlin)
scripts/                      device locks and an MCP call script for checks
docs/                         design, agent guide, MCP reference and notes
```

## Documents

- [Acceptance checklist](docs/ACCEPTANCE.md)
- [Design](docs/DESIGN.md)
- [Agent guide (SKILL.md)](docs/SKILL.md) and [MCP reference](docs/mcp.md)
- [Working on Pinky](docs/WORKING.md)
- [Usage statistics, crash reports and feedback](docs/TELEMETRY.md)
- Notes: [integration](docs/notes/integration.md), [gateway](docs/notes/gateway.md), [Android](docs/notes/android.md), [companion](docs/notes/companion.md), [iOS](docs/notes/ios.md), [client](docs/notes/client.md), [updates](docs/notes/updates.md), [telemetry](docs/notes/telemetry.md)
- [Changelog](CHANGELOG.md)

## Community

Ask questions, share what your agent does on your phone and follow development on [Discord](https://discord.gg/S27b7SHjng). Bugs and feature requests are welcome as GitHub issues; Help › Send Feedback… in Pinky opens one with diagnostics filled in.

## License

Apache-2.0
