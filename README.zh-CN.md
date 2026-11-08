<p align="center"><img src="assets/icon/pinky-macos-1024.png" width="112" alt=""></p>
<h1 align="center">Pinky</h1>
<p align="center"><b>让任何 AI Agent 用上你的 iPhone 和 Android 手机：Mac、Windows、Linux 都能跑，关键一步你说了算。</b></p>
<p align="center"><a href="https://github.com/DIYgod/pinky/releases/latest">下载</a> · <a href="#开始使用">开始使用</a> · <a href="#常见问题">常见问题</a> · <a href="https://discord.gg/S27b7SHjng">Discord</a></p>
<p align="center"><a href="README.md">English</a> · 简体中文</p>
<p align="center"><img src="assets/readme/demo-zh.gif" width="760" alt="Claude Code 在短信里输入回复，Pinky 扣住「发送」这一下，等你允许后才发出去"><br><sub>在 Android 16 模拟器上真实运行的 Claude Code。Pinky 默认只在付款前询问，这次还设了发消息前也要问。</sub></p>

Pinky 让任何 Agent（Claude Code、Claude Desktop、Codex、Cursor，或者一段脚本）通过 MCP 操作真实的 iPhone 或 Android 手机。你在电脑上看着它干活，有风险的步骤由你批准，随时可以接管，事后还能回看。审批由 Pinky 的网关执行，不靠给 Agent 的指令。

- **全平台**：一个应用，macOS（Apple 芯片和 Intel）、Windows、Linux 都能用。能操作 Android 手机（Android 10 到 17）和 iOS 17.4 及以上的 iPhone：iOS 27 及以上手机上什么都不用装；iOS 17.4 到 26 要从 Xcode 装一次辅助应用，iOS 27 上也可以选择用它（在 iOS 27 的真机和模拟器上试过，还没在 iOS 27 以下的真机上试过；[详情](#手机)）。
- **小巧原生**：窗口由 [MyGo](https://github.com/egoist/mygo) 在 GPU 上绘制，没有 Electron，也没有 webview：安装包 16–22 MB，在 Mac 上空闲时内存约 70 MB，CPU 0%。
- **任何 Agent，一键接入**：在「设置 › 智能体」里点一下就能接入 Claude Code、Claude Desktop、Codex、Cursor 等；任何支持 MCP 的 Agent 都能用，在同一台电脑上或者通过 HTTP 都行。
- **手机锁着也能在电脑上解锁**：在 Pinky 里保存一次手机的 PIN 码，之后每次解锁用 Touch ID、Windows Hello 或密钥环密码确认。Agent 永远看不到 PIN 码。
- **开源免费**，Apache-2.0 协议，不用注册账号。

<table>
<tr>
<td width="50%" valign="top"><img src="assets/readme/card-approval-zh.jpg" width="100%" alt="哪些操作要先问，你来定：Pinky 的窗口扣住 Agent 的「发送」，等你点「允许」"><br><b>有风险的步骤由你批准。</b>付款、交易和转账会等你点头；在「设置 › 安全」里，还可以让发送、下单、删除、打电话、装 App、跳出 App 的链接和更改设置也先问你，或者每一步都问。Pinky 能认出 10 种语言里的这些按钮。规则在 Pinky 的网关里执行，Agent 没法自己批准，屏幕上的文字想说服它也没用。</td>
<td width="50%" valign="top"><img src="assets/readme/card-off-limits-zh.jpg" width="100%" alt="这些它碰不到：「设置 › 安全」里的询问方式和已屏蔽的应用"><br><b>默认碰不到。</b>银行、钱包和密码管理器类 App 对 Agent 隐身，Agent 也不能往密码框和验证码框里输入。Agent 在任务中需要用银行或钱包 App 时会先问你，你可以只在这次任务里放行这个 App。每个新 Agent 都要你批准，只能拿到你给的权限范围。</td>
</tr>
<tr>
<td width="50%" valign="top"><img src="assets/readme/card-second-screen-zh.jpg" width="100%" alt="它用第二块屏幕，手机还是你的：Agent 的屏幕和手机自己的主屏幕并排"><br><b>手机还是你的。</b>在 Android 13 及以上，Agent 在隐藏的第二块屏幕上干活，手机自己的屏幕照常归你用，也可以熄屏锁着。较老的 Android 手机和 iPhone 上，Agent 用的是主屏幕。</td>
<td width="50%" valign="top"><img src="assets/readme/card-take-over-zh.jpg" width="100%" alt="随时接管：你在操作时 Claude Code 暂停，直到你交还"><br><b>随时接管。</b>点一下投屏，Agent 就暂停，直到你交还；在 Android 上，你亲手碰一下手机也会让它暂停。「全部停止」的快捷键是 ⌥⌘.（Windows 和 Linux 上是 Ctrl+Alt+.），在哪儿都能按。</td>
</tr>
<tr>
<td width="50%" valign="top"><img src="assets/readme/card-recording-zh.jpg" width="100%" alt="每一步都有记录：一次任务的步骤和它的录屏"><br><b>每次运行都有记录。</b>每一步都用大白话记下来，配上截图和录屏，录屏上每一步都有标记，还能导出成 HTML 报告。这些都只存在你的电脑上。</td>
<td width="50%" valign="top"><img src="assets/readme/card-iphone-zh.jpg" width="100%" alt="iPhone 上也一样：Pinky 扣住照片里的「删除」，等你点「允许」"><br><b>iPhone 也一样。</b>同样的网关、审批和记录，iOS 17.4 及以上都能用（<a href="#手机">能做什么</a>）。图里是 iOS 模拟器，设了删除前要问。</td>
</tr>
</table>

**状态**：pre-alpha。Claude Code 已经通过它在真机上完整跑通过（一台用 USB 连接的 Android 手机，和一台 iOS 27 的 iPhone），Agent 可以通过 stdio 或 HTTP 连接。从电脑解锁、Android 后台工作、录屏、导出、Wi-Fi 配对和手机文件都已经做好，其中一部分还在等人实际试用。现在哪些能用、怎么试，见 [docs/ACCEPTANCE.md](docs/ACCEPTANCE.md)。

## 开始使用

装好 Pinky，连上手机，再把 Pinky 接入你的 Agent。然后让 Agent 在手机上做点什么，在 Pinky 的窗口里看它干活。

### 需要准备什么

| | 需要 |
| --- | --- |
| **Mac** | macOS 12 或更高版本，Apple 芯片或 Intel 都行 |
| **Windows** | Windows 10 或 11，64 位（x64） |
| **Linux** | 64 位 PC（x86-64），Ubuntu 20.04、Debian 11 或更新的系统（glibc 2.29 或更高），以及 GTK 3 |
| **Android 手机** | Android 10 到 17，一根 USB 线。Pinky 自带 adb；Windows 上个别手机要装 USB 驱动，Linux 上要开放 USB 访问权限（[见下文](#windows-和-linux-上的-usb)） |
| **iPhone** | iOS 17.4 或更高版本，一根 USB 线。Pinky 用 Apple 的开发者磁盘映像准备 iPhone：映像随 Xcode 提供，你也可以让 Pinky 从第三方镜像下载。iOS 27 上 Pinky 暂时还不能自己挂载映像：插着 iPhone 打开一次 Xcode，映像就会一直在（[见下文](#iphone)）。iOS 17.4 到 26 还要在 iPhone 上装一个辅助应用（[见下文](#ios-174-到-26-的-iphone)） |
| **Agent** | 任何能用 MCP 服务器的 Agent，比如 Claude Code、Claude Desktop、Codex 或 Cursor |

### 1. 下载并安装 Pinky

从[最新发布](https://github.com/DIYgod/pinky/releases/latest)下载 Pinky：

- **Mac**：Apple 芯片（M1 或更新）的 Mac 下载 `pinky-<version>-arm64.dmg`，Intel Mac 下载 `pinky-<version>-amd64.dmg`。打开后把 Pinky 拖进「应用程序」。用 [Homebrew](https://brew.sh) 的话，`brew install --cask diygod/tap/pinky` 也一样。
- **Windows**：下载 `pinky-setup-<version>-amd64.exe` 并运行。它会把 Pinky 装在你自己的账户下，不需要管理员权限。安装程序暂时还没有签名，所以 SmartScreen 可能会提示「Windows 已保护你的电脑」：点「更多信息」，再点「仍要运行」。不想安装的话，解压 `pinky-<version>-windows-amd64.zip`，打开里面的 `Pinky.exe` 就行。
- **Linux**：Debian 和 Ubuntu 下载 `pinky_<version>_amd64.deb`，用 `sudo apt install ./pinky_<version>_amd64.deb` 安装。其他系统把 `pinky-<version>-linux-amd64.tar.gz` 解压到一个单独的文件夹，运行里面的 `pinky`。

Pinky 会自己检查新版本；Mac 版和用 Windows 安装程序装的 Pinky 还会自己装好更新（见[更新](#更新)）。

### 2. 连接手机

打开 Pinky，选「添加手机」。准备好的手机会出现在 Pinky 的窗口里；还缺什么的话，手机页面会告诉你。

#### Android

「添加手机 › Android」会按你手机的品牌给出下面这些步骤，用的是这个品牌自己的菜单名称，也会列出要额外打开的开关：小米、OPPO、vivo、荣耀、华为、三星和 Pixel，其他品牌给通用步骤。

1. 在手机上打开 USB 调试：在「设置 › 关于手机」里连续点「版本号」7 次，然后在「设置 › 系统 › 开发者选项」里打开「USB 调试」。有些品牌的菜单名称略有不同。
2. 把手机插到电脑上。手机问是否允许 USB 调试时，点「允许」。Android 11 及以上也可以在「添加手机」里改用 Wi-Fi 配对。
3. Pinky 会提出给手机装 Pinky Companion。这是它在手机上的应用，能让读屏快很多倍，还能让 Pinky 输入任何语言。手机可能会让你允许安装一次。

每台手机都有「体检」，在投屏下方的 ··· 菜单里。它会列出哪些能用、没通过的怎么改，比如小米的「USB 调试（安全设置）」开关。

<table>
<tr>
<td width="50%" valign="top"><img src="assets/readme/add-android-zh.png" width="100%" alt="选了小米的「添加手机 › Android」：在哪里打开开发者选项，以及要打开的三个 USB 调试开关"><br><sub>「添加手机 › Android」，选了小米。</sub></td>
<td width="50%" valign="top"><img src="assets/readme/checkup-zh.png" width="100%" alt="一台 Redmi 的体检：adb、连接和调试都通过了，Pinky Companion 和点按输入要处理，并给出了步骤"><br><sub>一台 Redmi 的「体检」，有两项要处理。</sub></td>
</tr>
</table>

#### Android 工具

Pinky 自带 adb，这是它连接 Android 手机用的工具，从 Android 源码构建，所以不用另装任何东西。如果电脑上已经有 Android 的 platform tools（来自 Android Studio、Homebrew 或系统的软件包），Pinky 会用它们，并和 Android Studio 等工具共用同一个 adb 服务。

#### Windows 和 Linux 上的 USB

- **Windows**：手机的 USB 调试需要驱动。很多手机 Windows 会自动装好驱动；没有的话，Pinky 会把这台手机标成需要设置，并告诉你装哪个驱动：Pixel 装 [Google USB 驱动](https://developer.android.com/studio/run/win-usb)，三星手机装[三星的驱动](https://developer.samsung.com/android-usb-driver)，其他手机装[手机厂商的驱动](https://developer.android.com/studio/run/oem-usb)。装好后拔下手机再插上。
- **Linux**：电脑要允许 Pinky 打开手机的 USB 设备，这需要 Android 手机的 udev 规则：Debian 和 Ubuntu 上运行 `sudo apt install android-sdk-platform-tools-common`，Arch 上装 `android-udev`，Fedora 上装 `android-tools`。可能还要把自己加进 `plugdev` 组。然后重新插一下手机。缺这些时 Pinky 会提示。

#### iPhone

Pinky 会自己准备好 iPhone，只是 iOS 27 上暂时有一步要用 Xcode（见下文）；「添加手机 › iPhone」会一步步带你操作。

1. 把 iPhone 插到电脑上，解锁，点「信任」并输入锁屏密码。在 Windows 上要先从 Microsoft Store 装「Apple 设备」（或者 iTunes）；在 Linux 上要装 `usbmuxd` 软件包。缺这些时「添加手机」会提示。
2. 在 iPhone 上打开「设置 › 隐私与安全性」里的「开发者模式」：这个开关是 Pinky 让它出现的。iPhone 会重启；解锁后点「打开」，再输入锁屏密码。
3. Pinky 把 Apple 的开发者磁盘映像挂载到 iPhone 上，然后把它列为就绪。iOS 27 的 iPhone 重启后映像还在；iOS 27 以下的 iPhone 每次重启后，只要你解锁，Pinky 都会自己重新挂载。

开发者磁盘映像随 [Xcode](https://apps.apple.com/app/xcode/id497799835)（Apple 给开发者的免费应用）提供，Pinky 不自带。在 Mac 上装一次 Xcode 并打开一次：Pinky 会用它装好的映像，之后 Xcode 不用再打开。在 Windows 和 Linux 上，把这样一台 Mac 上的 `/Library/Developer/DeveloperDiskImages/iOS_DDI` 复制到 Pinky 的文件夹里（[放在哪](docs/notes/ios.md#preparing-a-phone-without-xcode)）。两样都没有的话，「添加手机」会提出从第三方镜像 github.com/doronz88/DeveloperDiskImage 下载映像（Xcode 27.0 beta 4，32 MB，由 pymobiledevice3 的作者维护，不是 Apple），只在你点「下载」时才下载；Pinky 会按预期的校验和核对每一个文件。iOS 27 及以上，iPhone 上不会装任何应用。

在 iPhone 13 mini（iOS 27.0.1）上验证过：找到 Xcode 的映像，沿用手机上已有的映像，以及 iPhone 重启后映像还在。Pinky 自己挂载映像在这台手机上还不行：安装会一直等到超时。在修好之前，请在插着 iPhone 时打开一次 Xcode，让它挂上映像，之后映像会一直在。还没在真机上试过：下载的映像、开发者模式开关、iOS 27 以下的 iPhone，以及在 Windows 或 Linux 上连接 iPhone。

#### iOS 17.4 到 26 的 iPhone

iOS 27 以下的 iPhone，只能通过手机上的一个辅助应用接收来自电脑的点按和输入：WebDriverAgent，要你用自己的 Apple ID 从 Xcode 构建并安装一次（[怎么装](docs/notes/ios.md#installing-the-webdriveragent-helper)）。用免费 Apple ID 的话，每 7 天要重新安装一次。目前还没在 iOS 27 以下的 iPhone 上试过。不装它的话，Pinky 只能显示和读取屏幕（限 Mac）、打开 App，不能点按、滑动或输入。不支持 iOS 17.3 及更早版本。

### 3. 把 Pinky 接入你的 Agent

在 Pinky 里打开「设置 › 智能体」，点你的 Agent 旁边的「接入」。Pinky 会在你的电脑上找到 Claude Code、Claude Desktop、Codex、Cursor、VS Code、Copilot CLI、Windsurf（Devin Desktop）和 Gemini CLI，并用应用自身的路径把自己加进 Agent 的 MCP 服务器：Agent 有自己的命令时用它的命令（比如 `claude mcp add`），否则写进它的设置文件，写之前先备份一份，而且只改 Pinky 那一项。它还会把 Pinky 的 Agent 指南 [SKILL.md](docs/SKILL.md) 装到 Agent 读取技能的位置。在接入 Agent 之前，Pinky 侧栏里会有一张卡片，帮你接入，或者告诉你去哪里获取 Agent。

<p align="center"><img src="assets/readme/agents-zh.png" width="620" alt="「设置 › 智能体 › 接入智能体」：Claude Code 已接入；Claude Desktop、Codex、Cursor 和 VS Code 旁边是「接入」按钮"></p>

- **Claude Desktop**：「接入」会在 Claude Desktop 里打开 Pinky 的扩展，在那里点「Install」。双击[发布页](https://github.com/DIYgod/pinky/releases/latest)上的 `pinky-<version>.mcpb` 效果一样。
- **其他 Agent**：「设置 › 智能体 › 手动添加智能体」会给出适用于你这份 Pinky 的命令，旁边有复制按钮。添加一个运行这条命令的 MCP 服务器（stdio）。用 Homebrew cask 或 Linux 软件包安装时，JSON 配置写成：

  ```json
  { "mcpServers": { "pinky": { "command": "pinkyctl", "args": ["mcp"] } } }
  ```

  Claude Code 在终端里运行：`claude mcp add --scope user pinky -- pinkyctl mcp`。大多数 Agent 从 `~/.agents/skills/pinky/SKILL.md` 读取指南，Claude Code 从 `~/.claude/skills/pinky/SKILL.md` 读取。

命令是 `pinkyctl` 而不是 `pinky`：很多电脑上已经有另一个叫 `pinky` 的程序。另一台电脑上或容器里的 Agent 可以改用 HTTP 连接（[怎么连](docs/mcp.md#over-http)）。「接入」对每个 Agent 具体改了什么，见 [docs/mcp.md](docs/mcp.md#connecting-an-agent)。

### 4. 试一试

让 Agent 在你的手机上做点什么，比如「打开手机上的短信，告诉 Sam 我要晚点到」。Agent 第一次连接时，Pinky 会问你是否允许，以及允许它做什么。付款会在 Pinky 的窗口里等你点头；在「设置 › 安全」里，还可以让发送、删除等操作也先问你。

手边没有手机？退出 Pinky，再用两台脚本模拟的演示手机启动它（有一笔待批准的付款、一个被屏蔽的银行 App 和一个密码框）：Mac 上运行 `/Applications/Pinky.app/Contents/MacOS/Pinky --fake`，Linux 上运行 `pinkyctl --fake`，Windows 上在 PowerShell 里运行 `& "$env:LOCALAPPDATA\Programs\Pinky\Pinky.exe" --fake`。

## 使用 Pinky

### 手机

- **Android**：通过 USB 或 Wi-Fi 连接（[见上文](#android)）。Pinky 通过 adb 和内置的 scrcpy server 工作。它的伴侣应用随 Pinky 一起提供，读一次屏约 70 毫秒（不装要 2 秒），输入任何语言都不经过剪贴板：手机页面会提出帮你安装（手机可能会让你允许一次）。它的无障碍服务只在 Agent 操作手机时开启，因为银行 App 不肯和这样的服务一起运行（[原因](#常见问题)）。在 Android 13 及以上，Agent 在后台显示器上工作，手机自己的屏幕可以熄屏锁着。
- **iPhone**：通过 USB 连接，由 Pinky 准备好（[见上文](#iphone)）。Pinky 能做什么，取决于 iOS 版本和连接方式；连接方式可以在「设置 › 手机」里为每台 iPhone 单独选（「自动」按版本选，见下表）：

  | iOS | 方式 | 能做什么 | 测试过的设备 |
  | --- | --- | --- | --- |
  | 27 及以上 | Xcode 的 Device Hub 协议；手机上什么都不装 | 全部功能。输入文字时会先粘贴，再把你的剪贴板恢复原样 | iPhone 13 mini，iOS 27.0.1 |
  | 17.4 及以上，装了辅助应用 | WebDriverAgent 辅助应用，从 Xcode 装一次（[怎么装](docs/notes/ios.md#installing-the-webdriveragent-helper)）：iOS 17.4 到 26 自动使用，iOS 27 上由你选 | 全部功能，而且手机不共享屏幕，所以拒绝屏幕共享的银行 App 照常能用（测试中有星展银行 digibank 和招商银行）。比 Device Hub 慢：读一次主屏幕约 7 秒（Device Hub 不到 0.5 秒），点一下要 0.5 到 0.8 秒。辅助应用启动前，iOS 偶尔会要求在手机上输入锁屏密码。实时画面需要 macOS | iPhone 13 mini，iOS 27.0.1，选了辅助应用；iOS 18.5 和 26.5 模拟器。还没在 iOS 27 以下的真机上试过 |
  | 17.4 到 26，不装辅助应用 | 只用开发者服务；什么都不装 | 看画面（每秒几帧，限 macOS）、读屏幕（位置靠 OCR，限 macOS）、打开 App。不能点按、滑动、输入或回到主屏幕：没有辅助应用，手机不接受输入 | 同样的服务在 iOS 27.0.1 上测过；还没在 iOS 27 以下的真机上试过 |
  | 17.3 及更早 | | 不支持：17.4 之前的连接需要管理员权限，iOS 16 及更早还需要当前 Xcode 已不再包含的开发者磁盘映像 | |

  辅助应用是 Appium 的 WebDriverAgent，用你自己的 Apple ID 签名；用免费 Apple ID 的话，每 7 天要重新安装一次。
- **iPhone 的屏幕共享**：Pinky 串流 iPhone 的画面时，iOS 会告诉各个 App 屏幕正在共享，有些银行 App（比如星展银行 digibank 和招商银行）会拒绝运行或者发出警告。Pinky 只在这些时候串流：手机页面或卡片在 Pinky 里打开着、有 Agent 在操作它，以及之后的 30 秒；过后这些 App 就恢复正常。通过 CoreDevice 连接时，Agent 用不了这些 App。改用 WebDriverAgent 辅助应用连接这台 iPhone（「设置 › 手机」）：这样手机不共享屏幕，Agent 也能用这些 App。
- **锁着的手机**：Pinky 从不让手机一直亮着，只在每一步之前唤醒屏幕。在「设置 › 手机」里保存手机的 PIN 码，就能在电脑上用 Touch ID 解锁。Agent 永远看不到 PIN 码。Android 上，这在 AOSP 风格的锁屏上验证过（Mi 10 Pro 上的 LineageOS，Android 10、16 和 17 模拟器）；小米、三星、OPPO、一加、vivo、荣耀和华为的锁屏只有根据已知信息写的测试覆盖，Pinky 读不准 PIN 码键盘时什么都不会输入（[详情](docs/notes/integration.md#unlocking-other-android-phones)）。
- **移除手机**：手机 ⋯ 菜单里的「移除这台手机…」（手机离线时它的页面上、「所有手机」里它的卡片上、侧栏里它的右键菜单和「设置 › 手机」里也有）会把它从列表里去掉，并删除它保存的 PIN 码和设置。它的任务、截图和录屏会留在「活动」里，除非你勾选把它们也删掉；正在这台手机上运行的任务会停止。插着线的手机会立刻作为一台新手机回来，直到你拔掉它。通过 Wi-Fi 连接的手机会断开，并且不再出现在列表里，即使 adb 自己又连上了它；要加回来，就在「添加手机」里重新配对，或者插上线。Pinky 不会解除手机和电脑的配对：它的 USB 调试或无线调试配对，以及 iPhone 的信任，都会留给其他应用使用（[详情](docs/notes/gateway.md#removing-a-phone)）。

### 更新

Pinky 在启动时和之后每隔几小时检查一次新版本，有新版本时会显示一张卡片，写着更新了什么，附「立即更新」和「以后再说」。打开「设置 › 高级 › 自动更新」时（默认打开），它会在没有 Agent 在干活、你也有十分钟没用 Pinky 时自己装好，还会更新空闲的 Android 手机上的 Pinky Companion，更新后它的无障碍服务保持关闭，直到下一个任务。截至 2026 年 11 月 8 日的版本都用公开的密钥签名，遇到这样的 Pinky Companion，Pinky 会卸载它，重新安装用 Pinky 私有密钥签名的版本，并告诉你。更新之后，「新功能」会列出自你原来的版本以来的变化（来自 [CHANGELOG.md](CHANGELOG.md)）。

- **macOS**：Pinky.app 会先核对下载文件的校验和、确认它由 Pinky 的开发者签名并经过 Apple 公证，再替换自己；新版本启动不了就退回旧版本。Homebrew cask 装的那份也会这样自己更新，它已经是最新时 `brew upgrade` 不会动它。直接从磁盘映像里运行的、或者放在你没有写权限的文件夹里的 Pinky，会告诉你怎么更新。
- **Windows**：用安装程序装的 Pinky 会在核对校验和后静默运行新的安装程序，启动新版本，新版本启动不了就退回旧版本。免安装版会帮你下载安装程序。
- **Linux**：Pinky 会把新版本下载好，由你来安装。
- **终端**：Pinky 没在运行时，`pinkyctl update` 会安装更新；`pinkyctl daemon` 只会在日志里提示有新版本。

详情见 [docs/notes/updates.md](docs/notes/updates.md)。

### 使用统计、崩溃报告和反馈

Pinky 会发送匿名的使用统计（只有计数，比如跑了几个任务、调用了哪些工具、连了几台手机，大约一天一次，带一个随机的安装 ID）和崩溃报告；可以在「设置 › 隐私」里把两者都关掉，或者设置 `DO_NOT_TRACK=1`。屏幕内容、输入的文字、App 名、手机名和标识、PIN 码和文件名都不会发送。「帮助 › 发送反馈…」会帮你打开一个 GitHub issue，填好你的留言和诊断信息，你看过后自己提交。完整清单见 [docs/TELEMETRY.md](docs/TELEMETRY.md)。

## 常见问题

<details>
<summary><b>哪些 Agent 能用 Pinky？</b></summary>

任何能用 MCP 服务器的 Agent：通过 stdio 运行 `pinkyctl mcp`，或者用智能体令牌通过 HTTP 连接（[怎么连](docs/mcp.md#over-http)）。「设置 › 智能体」能一键接入 Claude Code、Claude Desktop、Codex、Cursor，以及它在你电脑上找到的其他 Agent。目前在真机上完整跑通的是 Claude Code。Pinky 自己不带模型，动脑子的是你的 Agent。

</details>

<details>
<summary><b>Agent 能自己批准有风险的操作，或者被人骗着批准吗？</b></summary>

不能。规则在 Pinky 的网关里执行，网关挡在 Agent 和手机之间。需要你批准的步骤会以 `approval_pending` 返回给 Agent，你在 Pinky 里点「允许」之前，手机上什么都不会发生；Agent 也没有任何东西可以发来替你确认。真正执行前，Pinky 还会核对按钮是否还在原处，60 秒内没回应就当作拒绝。Pinky 靠按钮上的文字认出这些按钮，支持 10 种语言，长得特别的按钮可能会漏掉：想更严格，「设置 › 安全」里有「在每个操作前都询问」和「只读模式」。

</details>

<details>
<summary><b>哪些东西 Agent 看不到？</b></summary>

银行、钱包和密码管理器类 App（Pinky 自带一份名单，可以在「设置 › 安全」里添加）：Agent 拿不到它们的画面，它们的通知也会被藏起来。Agent 不能往密码框和验证码框里输入，通知里的验证码会被打码。Agent 在任务中需要用银行或钱包 App 时，Pinky 会先问你。你允许的话，Agent 在这次任务剩下的时间里可以打开和读取这个 App，你的规则在那里照样会问，Pinky 也不会留下它的截图和录屏。密码管理器不会提供这个选项。

</details>

<details>
<summary><b>手机屏幕会被传到云端吗？</b></summary>

Pinky 不会传。截图、录屏和记录都存在你的电脑上，默认保留 30 天，可以在「设置 › 隐私」里修改，也可以在那里删除。Agent 读到的内容会发给你选的那个 Agent，这和你给它用任何工具一样。Pinky 的匿名使用统计从不包含屏幕内容、输入的文字或 App 名（[见上文](#使用统计崩溃报告和反馈)）。

</details>

<details>
<summary><b>Pinky 会在手机上装什么？</b></summary>

Android 上，你点「安装」后会装上 Pinky Companion，之后 Pinky 会帮它保持最新，并且只在 Agent 工作时开启它的无障碍服务（[原因](#常见问题)）。iPhone 上，iOS 27 及以上不装任何应用：Pinky 只是像 Xcode 那样挂载 Apple 的开发者磁盘映像（iOS 27 上暂时还要用 Xcode 挂载一次，[详情](#iphone)）。iOS 17.4 到 26 上是 WebDriverAgent 辅助应用，要你从 Xcode 装一次（[怎么装](docs/notes/ios.md#installing-the-webdriveragent-helper)）。

</details>

<details>
<summary><b>银行等 App 会发现 Pinky Companion 吗？</b></summary>

无障碍服务开着的时候，它们能发现。恶意软件常常滥用无障碍服务，所以很多银行 App 在发现某个不是从应用商店装的 App 开着无障碍服务时，会拒绝登录：马来西亚的银行（Maybank、CIMB、HSBC 以及马来西亚银行公会的其他成员，2024 年起）和新加坡的银行（DBS、OCBC、UOB，2023 年起）都是这样，其他地方也有。Pinky Companion 就是这样的 App，因为它是 Pinky 通过 USB 装上的。微信、抖音、小红书这类 App 也能看到这个服务，并且会留意自动化操作；我们还没听说它们因为 Pinky Companion 采取过措施，但让 Agent 操作它们可能违反它们的用户协议。

所以 Pinky 只在 Agent 操作手机时才开启这个服务：任务开始时开启，任务结束或你接管手机后几秒关闭，Agent 一分钟没有操作也会关闭。其余时间它都是关着的，这些 App 照常能用。Agent 工作期间（哪怕是在手机的后台显示器上），它们能看到这个服务：这时要用它们，先在 Pinky 里点「接管」，或者结束任务。

如果完全不想让 Pinky 开启这个服务，在手机上卸载 Pinky Companion（Android 的「设置 › 应用」）。没有它 Pinky 照样能用，只是慢一些：读一次屏约 2 秒而不是 70 毫秒，输入英文以外的文字要经过手机的剪贴板。手机页面上的「以后再说」可以收起 Pinky 重新安装它的提示。

</details>

<details>
<summary><b>Pinky 和其他给 Agent 用的手机工具有什么不同？</b></summary>

做 Pinky 之前，我们看了十几个同类方案。没有一个开源项目能同时做到：iPhone 和 Android 都支持，macOS、Windows、Linux 都能跑，能接任意 Agent，并且让人在回路里。多数方案要么靠 Agent 自觉先问你，要么只能整类拦截；Pinky 在 Agent 之外扣住有风险的那一下点按，并且记录每一次运行。

</details>

<details>
<summary><b>把手机交给能执行命令的 Agent 安全吗？</b></summary>

能以你的身份执行命令的 Agent，可以绕开 Pinky 直接接触手机（比如调用 adb），从而跳过它的规则。这类 Agent 请放在接触不到手机的沙盒里运行，或者把手机插到另一台电脑上，让 Agent 通过 HTTP 连接那台电脑上的 Pinky（[怎么连](docs/mcp.md#over-http)）。

</details>

## 参与开发

用 Pinky 的话，装发布版就行（见[开始使用](#开始使用)）。自己构建需要 Go 1.27.3 或更高版本，Mac 上还要 Xcode 命令行工具。

```sh
make check   # 构建（CGO_ENABLED=0）、vet、测试
make app     # macOS：带 FFmpeg 和伴侣应用的 Pinky.app，以及 .dmg
```

各系统的构建步骤、FFmpeg 和 adb 的打包方式、测试和代码结构，见英文版的 [Build from source](README.md#build-from-source)、[Build and test](README.md#build-and-test) 和 [Code layout](README.md#code-layout)。开发文档都是英文的：

- [验收清单](docs/ACCEPTANCE.md)
- [设计](docs/DESIGN.md)
- [Agent 指南（SKILL.md）](docs/SKILL.md)和 [MCP 参考](docs/mcp.md)
- [参与开发须知](docs/WORKING.md)
- [使用统计、崩溃报告和反馈](docs/TELEMETRY.md)
- [更新日志](CHANGELOG.md)

## 社区

欢迎来 [Discord](https://discord.gg/S27b7SHjng) 提问、分享你的 Agent 在手机上做了什么、关注开发进展。Bug 和功能建议也欢迎在 GitHub 提 issue；Pinky 里的「帮助 › 发送反馈…」会帮你填好诊断信息。

## 许可证

Apache-2.0
