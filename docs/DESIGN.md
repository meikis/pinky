# Pinky — Product and Technical Design

Source of truth while drafting: https://claude.ai/artifact/1tETTimeUQMqvjvQf5ALsn

Nov 6, 2026 · @DIYgod · Draft

## Summary

Pinky lets any AI agent operate a real iPhone or Android phone while a person watches, approves risky steps, takes over and reviews what happened. It is a local gateway plus a desktop console: agents connect over MCP, and the console mirrors the screen, holds approvals and keeps the history.

Key decisions:

- **Bring your own agent.** Pinky ships no model. Claude Code, Codex, Cursor or a script connects through MCP, a CLI or an SDK.
- **Safety lives in the gateway.** Policies, approvals, blocked apps and read-only mode apply to everything an agent does through Pinky, and only the person can approve. An agent that can run shell commands as the same user could still reach the phone through adb; the threat model covers how to isolate it.
- **One Go program.** The gateway, the MCP shim and the desktop client ship as one Go binary; the client uses MyGo's native UI.
- **iOS with nothing installed on the phone.** On iOS 27+, Pinky speaks the CoreDevice protocol behind Xcode's Device Hub (screen stream and touch) through go-ios. WebDriverAgent covers older iOS.
- **Android through adb and scrcpy**, plus a small companion app for a fast accessibility tree, events and text input.
- **Local-first.** Video, screenshots and history stay on the user's machine by default.
- **Android ships first.** Step 0, a one-to-two-week validation before M1 (see Roadmap), measures the risky parts. iOS follows in M3, and step 0 decides whether it launches on Windows and Linux as well as macOS.

Status: draft design, nothing built yet. "Pinky" is the project name.

## Landscape

No project, open or closed, offers all four: iOS and Android, a Windows/macOS/Linux client, bring-your-own agent, and a person in the loop. The closest, AndroMeld, is closed source and covers only Mac and Android. The appendix compares the eleven projects reviewed.

What no one does: hold a single "Send" or "Pay" tap until a person approves it, outside the agent's reach. Poki and mirroir-mcp block whole actions or apps, and AndroMeld and iPhone-use rely on instructions to the agent. Pinky's approvals are its main difference.

## Goals and non-goals

Pinky is the agent's hands on a phone and the person's guard rail, not another agent.

Goals:

1. Any agent can operate an iPhone or an Android phone through one interface, with the same screen format, statuses and errors on both.
2. A person sees what the agent does as it happens, approves risky steps, and can stop or take over at any moment.
3. Every task leaves a record the person can review and export, and later replay without a model.
4. Setup takes minutes on macOS, Windows and Linux, with nothing installed on the phone where the OS allows it.
5. Rules are enforced by the gateway, so a careless or prompt-injected agent cannot get around them.

Non-goals:

- A built-in agent or model.
- A device farm or CI service. Teams can script Pinky, but scheduling, sharding and cloud devices are out of scope.
- Rooting, jailbreaking, or defeating app protections such as root detection or screenshot blocking.
- Controlling phones the user does not own or administer.
- Hiding automation from apps or platforms.

## Users and key scenarios

Three groups come first: people who hand everyday phone chores to their agent, app developers who test on real devices, and small teams who run a few phones for operations.

| Scenario | User | What Pinky does | First works in |
| --- | --- | --- | --- |
| "Reorder last week's lunch" from Claude Code | Individual | Opens the delivery app, fills the cart, holds the Pay tap for approval | M1 |
| "Reply to Sam: Saturday at 3 works" | Individual | Types the reply; the Send tap waits for approval | M1 |
| "Check my boarding pass and add the gate to my calendar" | Individual | Read-only in the airline app, allowed writes in Calendar | M2, with per-app rules |
| "Walk through onboarding on iPhone 17 and Pixel 9, file what breaks" | App developer | Drives both devices in one session; the task history becomes the bug report | M3 |
| "Each morning, read three store dashboards and post a summary" | Small team | Runs on an office phone through a headless gateway; reading needs no approval, posting is approved from a remote console | M4 |

## Product principles

When two choices compete, these decide.

1. **Bring your own agent.** Pinky never ships a model. A later "quick task" box may call the agent the person already has, in its headless mode.
2. **Enforce, don't instruct.** Every rule that matters runs in the gateway. Text in a tool description is guidance, never protection.
3. **Honest results.** An action that may have happened is reported as `uncertain` and never retried blindly.
4. **Light on the phone.** Prefer protocols to installed apps, and turn helpers on only during tasks.

## Features

Each feature is tagged with the milestone that first ships it (see Roadmap). A basic approval policy ships in M1, because approvals are what sets Pinky apart.

| Area | Feature | Milestone |
| --- | --- | --- |
| Devices | USB discovery and Android wireless debugging by QR code | M1 |
| Devices | Setup checks with fixes: Developer Mode, USB debugging, vendor notes (Xiaomi, Huawei) | M1 |
| Devices | Device card: connection, battery, lock state, foreground app, controlling agent | M1 |
| Devices | Keep the phone awake while plugged in, with its screen off | M1 |
| Devices | Unlock from the computer: the PIN is saved in the OS keychain and entered after Touch ID or Windows Hello; agents never see it | M1 |
| Devices | iPhone pairing over USB, then over Wi-Fi with a PIN where possible | M3 |
| Devices | iOS setup checks: Developer Mode, trust, developer disk image, tunnel prerequisites | M3 |
| Devices | Remote gateway on a home Mac mini or NAS, over Tailscale or a relay | M4 |
| Mirroring | Low-latency screen with mouse, wheel, keyboard and input methods; right click is Back; clipboard sync; screenshots | M1 |
| Mirroring | Agent overlay (tap ripples, target outline) and a status line with Take over and Stop | M1 |
| Mirroring | Takeover: input from the person pauses the agent; its next call returns `user_took_over` | M1 |
| Mirroring | Android background mode on a virtual display (M1 if step 0 confirms it works while the phone is locked) | M2 |
| Mirroring | Device grid, drag-and-drop of files and APKs, audio forwarding, element inspector | M2 |
| Agent access | MCP server (stdio shim) and local HTTP API; one-click setup for Claude Code, Codex and Cursor; skill file | M1 |
| Agent access | First connection needs approval and a scope: read-only, chosen apps, or everything | M1 |
| Agent access | Leases: release, idle expiry, revoke from the device card | M1 |
| Agent access | CLI and SDK for code mode (one script, many steps) | M4 |
| Agent access | Live screen inside MCP hosts that support MCP Apps | M4 |
| Safety | Default policy: commit detection, Ask categories, blocked finance apps; approval panel with Allow once, Deny and Take over; no answer in 60 s denies | M1 |
| Safety | Emergency stop (global shortcut and Stop all); read-only mode; per-task app allow-list | M1 |
| Safety | Typing into password and code fields refused; codes in notifications masked; notifications from blocked apps hidden | M1 |
| Safety | Rules editor, per-app rules, "Allow for this task" and "Always allow this kind" | M2 |
| Safety | Credentials filled from the OS keychain; wider redaction | M2 |
| Safety | Approve from a notification, a phone push or a chat app | M4 |
| History | Task timeline: each step in plain language, with before and after screenshots, approvals and, in Developer mode, the tool call; wipe | M1 |
| History | Continuous recording with step markers; search; HTML report and JSON export; retention limits | M2 |
| History | Turn a successful task into a flow that replays without a model and hands back to the agent on a mismatch | M4 |
| History | History as an MCP resource; optional encryption at rest | M4 |
| Scale | Watch-only access, per-agent device permissions, queueing | M4 |
| Scale | Run one flow on many devices at once | Later |
| Scale | HarmonyOS driver (hdc) | Later |

## Roadmap

Android ships first, at M1, because it needs no new protocol work; iOS follows at M3. Each gate is the exit criterion of the phase above it, and a phase ships only after its gate passes.

(Diagram: see the design doc at https://claude.ai/artifact/1tETTimeUQMqvjvQf5ALsn)

Step 0 takes one to two weeks and checks six things:

1. Video through MyGo and FFmpeg at 1080p and 60 fps on macOS, Windows and Linux: latency, CPU and memory, idle and while mirroring.
2. go-ios on Windows and Linux: pair an iOS 27 device, start the display stream, inject taps.
3. The iOS element tree, pass or fail: list every element on a typical screen with label, role and frame in under 1.5 s. If it fails, OCR places labels and WebDriverAgent supplies frames where installed.
4. Settle detection from encoded frame sizes, against accessibility events.
5. The companion app's tree speed against `uiautomator dump`.
6. Locked phones: whether apps on scrcpy's virtual display keep running while an Android 13+ phone is locked, and whether the CoreDevice stream and touches work on a locked iPhone.

Step time runs from receiving a tool call to returning its settled result, excluding approval waits. M1 ships on macOS, Windows and Linux.

## Key screens and flows

The window has the same three parts whatever the number of phones: a sidebar listing the phones, the selected phone (or all of them) in the middle, and a panel on the right with what needs the person and the running tasks. One phone is just a list of one. Tool calls, refs and the element list appear only in Developer mode. Each screen is drawn with sample data in the [design prototype](https://claude.ai/artifact/EsCTP9Xsphcz8C6jbGsQM2).

| Part | Always | Only with 2+ phones |
| --- | --- | --- |
| Sidebar | One row per phone with a status dot and a plain status line; "+ Add phone" under the rows; Activity, Settings and Stop all at the bottom | An "All phones" row at the top |
| Center | The selected phone's mirror, showing state only (the target, an amber outline, locked, you're in control) and never approval buttons | All phones shows live tiles at lower resolution; clicking one selects it |
| Right panel | "Needs you": approval and unlock cards, each naming its phone. "Tasks": the selected phone's task, expanded, with Take over and Stop | Other phones' tasks, collapsed to one row each |

| Screen | Purpose |
| --- | --- |
| Phone | Watch and steer the selected phone's task |
| All phones | See several phones at once |
| Phone, locked | Unlock from the computer with Touch ID, or on the phone |
| Phone, you're in control | Pass control back and forth |
| Approval panel | The same Needs you card, floating when the window is not in front, plus the screenshot, since the mirror is out of view: the whole screen with the target marked, beside the part around the target large enough to read, and larger on a click |
| Menu bar flyout | The same sections as the right panel, plus Stop all |
| Activity | Review past tasks by day, with the phone on each row |
| Settings | Phones, Agents, Safety presets, Privacy, Advanced |
| Add phone | Connect a phone one step at a time; opens by itself when none is connected |

First run, in order:

1. Install Pinky and open it; with no phone connected, it opens straight into Add phone.
2. Add a phone: plug it in, or scan the wireless-debugging QR code.
3. Connect an agent with one click; Pinky writes its MCP config and skill file.
4. The agent's first connection asks the person for approval and a scope.
5. The person gives the agent a task; the Phone screen shows each step.
6. A commit such as Pay or Send appears as a Needs you card, and the person decides.
7. The task lands in Activity, ready to review or export.

Interaction rules:

- Everything that needs the person, approvals and unlocks alike, appears in one place: the Needs you section, or the floating panel when the window is not in front. The panel never takes keyboard focus; after the person clicks it, Return means Allow once and Esc means Deny.
- Touching the mirror pauses the agent and shows the "you're in control" banner. On Android, touches on the phone itself do too; on iPhone they cannot be detected, so the person uses the mirror or Stop.
- Developer mode adds a Details toggle to the expanded task card, with the tool calls and what the agent sees.
- Closing the main window keeps the gateway running in the menu bar.

## Agent interface

Agents see one MCP server, `pinky`, with 14 tools. Every action waits for the screen to settle and returns the new screen, or only what changed, so agents rarely call `phone_observe`.

| Tool | Arguments | Returns |
| --- | --- | --- |
| `phone_connect` | `device?`, `task` | A lease on the device and the first screen; the task name shows in the client |
| `phone_release` | `outcome?`, `summary?` | Ends the task and frees the device |
| `phone_observe` | `wait_ms?` (up to 30000), `screenshot?` | The current screen, plus a screenshot on request |
| `phone_tap` | `ref` or `x,y`; `long_press?` | Result and new screen |
| `phone_type` | `text` (up to 16384 chars); `ref?`; `append?`; `submit?` | Result and new screen |
| `phone_swipe` | `direction`, a `ref`, or `from` and `to`; `duration_ms` (80 to 1200) | Result and new screen |
| `phone_key` | `key`: back, home, recents, enter, tab, delete, escape, dpad keys, page\_up, page\_down | Result and new screen |
| `phone_launch` | `app`: name, bundle id or package | Result and new screen |
| `phone_apps` | `query?` | Installed apps inside the agent's scope |
| `phone_batch` | `steps[]` | One result per step; stops at the first step that is not `settled` |
| `phone_wait_for` | `text`, `gone?`, `timeout_ms?` (up to 45000) | The screen once the text appears or disappears |
| `phone_files` | none | A local folder synced with the phone's `Pinky/` folder (Android only) |
| `phone_request_approval` | `summary`, `details?` | The person's answer and note; it grants nothing by itself |
| `phone_handoff` | `reason` | Returns when the person hands control back, with their note |

Every action tool also takes `device?` (required when the session holds more than one device), `intent?` (one sentence on why, shown on the approval panel and in history) and `approval_id?` (to keep waiting on a pending approval).

### Sessions and leases

- A lease belongs to one MCP session, which may hold several devices. It ends on `phone_release`, when the session closes, after 10 idle minutes (configurable), or on Stop.
- Another session asking for a held device gets `lease_held` with the holder's task name. The person can revoke a lease from the device card.
- A device that drops mid-action returns `uncertain`, then `device_offline`; if it reconnects within 60 s, the lease survives.
- An MCP cancellation, or a session that closes, withdraws any open approval panel without acting.

### Screen format

```text
screen 1080x2400 · app: Dinely (com.dinely.app) · portrait · keyboard hidden · focus [14]
toast: "Added to cart"
[3]  button "Back" @40,88
[12] list "Cart" scrollable
[14] input "Note for the restaurant" @180,640 focused
[15] button "Place order · $12.80" @360,800
ocr "Delivery in 25–35 min" @200,300
```

- One element per line: `[ref] role "label" @x,y state`. Coordinates are pixels of the screenshot the agent would receive, whose size opens every full screen.
- A ref is a hash of role, label, ancestry and position among identical siblings; it stays the same while all four do.
- After an action the result is a diff: lines start with `+` (added), `-` (removed) or `~` (changed). A new app, or a diff longer than the screen, sends the full screen.
- Areas the accessibility tree cannot read (canvas, games, mini-programs, Flutter apps without semantics labels) are filled with `ocr` lines from local OCR. A screenshot is sent only on request or when OCR is not enough.
- Toasts, notifications and dialogs that appeared since the last result are listed above the elements, except notifications from blocked apps.
- Screen text is untrusted data. Pinky never acts on it, and the skill file (a `SKILL.md` that teaches agents the tools) says the same.

### Results

| Status | Meaning | What the agent should do |
| --- | --- | --- |
| `settled` | The action ran and the screen stopped changing, or settling hit its 3 s limit (`settle: timeout`) | Continue |
| `uncertain` | The action may have happened, for example a timeout after Send | Observe before anything else; never repeat it blindly |
| `blocked_by_policy` | A rule denied the action; the result names the rule | Choose another way or ask the person |
| `approval_denied` | The person said no; `note` may say why | Follow the note |
| `approval_pending` | The call blocked for 45 s; the result carries an `approval_id` | Repeat the same call with that id |
| `stale` | The screen changed while the approval waited, so nothing was done | Observe and decide again |
| `user_took_over` | The person used the phone; returned at once with the current screen | Call `phone_handoff` to wait for control and the note |
| `paused` | The person paused the agent | Wait with `phone_handoff` |
| `stopped` | The person pressed Stop; the lease is gone | End the task |
| `device_locked` | The screen is locked; the person can unlock from the computer or the phone | Call `phone_handoff` |

Errors: `device_offline`, `lease_held`, `scope_denied` (an app outside the agent's scope or the task's allow-list), `not_found` (the ref is gone), `unsupported` (the backend cannot do this, such as `recents` on iPhone).

Any call that could block longer than 45 s returns a pending status with an id instead, and the agent repeats the call with that id: Ask actions, `phone_request_approval`, `phone_handoff`, `phone_wait_for`, and long `phone_type` on iPhone. That stays under the 60 s request timeout common in MCP clients. Progress notifications are sent only when the request carries a `progressToken`.

### Example session

```text
→ phone_connect   { task: "Reorder last week's lunch" }
← settled         screen 1080x2400 · Pixel 9 · home screen
→ phone_launch    { app: "Dinely" }
← settled         app: Dinely · [7] tab "Orders" …
→ phone_tap       { ref: 7 }
← settled         + [21] button "Reorder · Chicken rice bowl"
→ phone_tap       { ref: 21 }
← settled         toast: "Added to cart" · + [30] button "Cart · 1"
→ phone_tap       { ref: 30 }
← settled         app: Dinely · [14] input "Note for the restaurant" · [15] button "Place order · $12.80" …
→ phone_tap       { ref: 15, intent: "Pay $12.80 for 1 chicken rice bowl" }
← approval_pending  after 45 s · approval_id: ap_7f3 · rule: Payments
→ phone_tap       { ref: 15, approval_id: "ap_7f3" }
← settled         allowed once · "Order placed"
→ phone_release   { outcome: "done" }
```

## Approvals and safety

Approvals are decided in the client and enforced in the gateway; the agent only sees the outcome. A prompt-injected agent therefore cannot approve its own action, which a consent parameter filled in by the agent cannot guarantee.

(Diagram: see the design doc at https://claude.ai/artifact/1tETTimeUQMqvjvQf5ALsn)

### Policy terms

- **Read-only:** the agent may observe, launch apps, tap, scroll and type into search fields; anything that would be Ask becomes Deny.
- **Watch-only:** observe only.
- **Kind:** the rule an approval matched, such as "Payments"; "Always allow payments in Dinely" keeps an exemption for that kind in that app (see [Always allow](#always-allow)).
- An app outside the agent's scope or the task's allow-list returns `scope_denied`; a blocked app or a Deny rule returns `blocked_by_policy`.

### Risk tiers

Defaults; the person can change each one. Settings › Safety › "When should agents ask you?" picks what asks: payments only (the default, recommended), payments, messages and deletions (the second Ask row too), or every action.

| Tier | Actions |
| --- | --- |
| Allow | Observe, navigate, scroll, search, type into search fields; by default also what the second Ask row lists |
| Ask | Pay: a commit on a screen that shows an amount (Pay, Place order · $12.80, a price in a store); write files to the phone |
| Ask, with "Ask before payments, messages and deletions" | Send a message or post; order, book or transfer; delete; change account or security settings; place a call; install or uninstall an app; open a link that leaves the app; read the clipboard |
| Deny | Anything in a blocked app (banking, wallets and password managers by default), where the agent does not even get the screen; typing into password and one-time-code fields |

### Detecting a commit

Policy actions are tool calls in context:

| Action | Detected as |
| --- | --- |
| `commit` | A tap whose label matches the commit words (Send, Pay, Order, Place order, Checkout, Buy, Sell, Swap, Trade, Repay, Borrow, Stake, Book, Reserve, Confirm, Submit, Delete, Remove, Bin, Transfer, Post), in every supported UI language; text that only OCR reads (a web view) by the whole line and by its pieces; never a tab, which only moves between pages; for a button with no words of its own (an icon), what the app calls it: the resource id on Android, the accessibility identifier or SF Symbol name on iOS (`btn_send`, `sendButton`, `paperplane.fill`, `trash`), minus ids that name a state or a place (`send_status`, `payment_methods`) |
| `payment` | A commit that moves money by itself (Pay, Buy, Sell, Swap, Trade, Repay, Borrow, Stake, Withdraw, Transfer, Deposit, 还款, 分期, 买入, 卖出, 赎回, 申购, 闪兑 and the like), or any commit on a screen that shows an amount, coins included (`0.5 ETH`, `1,200 USDC`). Convert, Exchange, Redeem and Claim count only next to an amount |
| `send_message` | Enter, or `submit: true`, in an input of a messaging app |
| `install_app` | Install, Get or Uninstall in an app store or the system's package dialog |
| `open_url` | A tap on a link element that leaves the current app |
| `call` | A tap on a call button, or a dialer showing a number |

App categories (finance, messaging, stores) come from a bundled, editable list of package names and bundle ids; an app the list does not know whose name says bank, wallet, securities, remittance or pay (in Latin script, Chinese, Japanese or Korean) counts as finance. `apps.yaml` can exclude an id or a name, which then beats a prefix entry and the name (Settings › Safety unblocks one app that way). The rules file is the only source of truth, and Settings › Safety edits it:

```yaml
rules:
  - name: Payments
    match: { action: payment }
    decision: ask
  - name: Send in Chatter
    match: { app: [com.chatter.app], action: [commit, send_message] }
    decision: ask
  - name: Finance apps
    match: { category: finance }
    decision: deny
  - name: Installs and links
    match: { action: [install_app, open_url] }
    decision: ask
```

When several rules match, the strictest wins: Deny, then Ask, then Allow; among rules with the same decision, the first one names the result. An Allow rule therefore never cancels another rule's Ask or Deny (Always allow does that for asks, below).

Settings › Safety › Rules lists the file's rules to add, change, move and remove, each with its name, what it matches (apps, categories, actions, words of the element's label) and what it does (Block, Ask, Allow). The rules Pinky adds show there read-only: the preset's, which come before the file's, the apps blocked in Settings, and the credentials rule. Before saving, Pinky checks a rule: a name no other rule has (the built-in ones included), something to match, known actions. Removing a rule that blocks, or changing what it matches or does, asks the person first; removing or renaming a rule drops the Always allow answers about it.

Edits of `rules.yaml` and `apps.yaml` made by hand while Pinky runs take effect without a restart (Settings › Advanced › Edit rules file opens `rules.yaml`). The gateway looks at both files every two seconds, and again before Settings shows the rules or changes either file, and reads a file again when it changed since Pinky last read or wrote it, once it has stayed the same for a moment, so that an editor's save half done is never taken for rules. Settings › Safety then shows what the files hold, its switches and blocked apps included, and a change in Settings passes on only what the person changed. A change from Settings is made on top of the edit, so it never undoes one, though Settings writes the file in its own layout, without the person's comments. Settings › Safety › Rules saves a change only while the file's rules are still the ones the change was made from; after a hand edit of the rules it saves nothing, says so, and shows the file's rules, and the person makes the change again. An edit that does not parse, or that has a rule or an Always allow answer Pinky cannot use (a rule without a name or anything to match, an unknown action), changes nothing: Pinky keeps the rules it had, and Settings › Safety and Advanced say what is wrong. Until the person fixes the file, the controls that would change it are off (and, for `rules.yaml`, approvals do not offer Always allow), so Settings never writes over the edit. A file that is removed starts again from the one Pinky ships, as at the first start.

### Approval panel and transport

- A floating panel that never takes focus shows the screenshot with the target highlighted, the agent's `intent`, the matched rule, the task and the agent. A system notification appears too; clicking it opens the panel.
- Choices: Allow (once) and Deny, and under More options: Allow for this task, Always allow *payments* in *Dinely*, Take over, Deny with a note.
- The timeout runs from when the panel opens: 60 s by default, then Deny. A tool call blocks for at most 45 s and then returns `approval_pending`; a decision made while no call is open is kept for the next call with that id until the timeout.
- While an approval is open, the device accepts no other action; `phone_observe` and `phone_wait_for` still work.
- An `approval_id` is single-use and bound to the device, lease, tool and arguments; a call with other arguments is refused.
- On approval the gateway re-checks the target's label and position and the screen text shown in the panel (status bar excluded). Any change returns `stale`, and nothing is done.
- `phone_request_approval` records the person's answer but grants nothing: a later step the rules ask about, such as a payment, still asks, and so does the agent's next question. Its card offers no Allow for this task (nor Always allow): a question is the person's consent to what it says, and answering the next ones by itself would let an agent claim consent the person never gave.

### Always allow

"Always allow payments in Dinely" keeps the person's answer in the rules file, under `always_allow`, as an exemption: in that app, the rule the approval named (Payments) and the rules that asked with it (Commits, with "Ask before payments, messages and deletions", since a payment is a commit) no longer ask about the steps that rule asks about, in later tasks and after a restart. Another commit in Dinely, such as Delete, still asks, as do a payment in another app and a step that a further rule asks about.

```yaml
always_allow:
  - rule: Payments
    with: [Commits]
    app: com.dinely.app
    app_name: Dinely
```

- The strictest verdict wins, so an allow rule could not cancel an ask. An exemption instead takes asks out of the verdict, after everything that denies: it never lifts a deny (blocked apps, finance and password apps, credential fields, the person's own deny rules), read-only mode, an agent's scope or a task's app list.
- It also allows the rest of the task, as Allow for this task does; that is what covers a task's app list, for that task only.
- It matches the app by package name or bundle id, and by name when the phone reports no id.
- While "Ask before every action" is on, exemptions do not apply and the panel does not offer Always allow; they apply again with another preset.
- The panel offers it only for actions the policy asked about in an app: not for the agent's own questions (`phone_request_approval`), copying files, or opening apps and links. Where it cannot be kept it counts as Allow for this task, and on the agent's own questions as Allow once.
- The answer is kept when the person gives it, even when the agent's call is no longer waiting for it.
- Settings › Safety › Always allowed lists the exemptions; removing one makes its rules ask again.
- The agent learns of it: the approved call says the rule no longer asks in that app, and a later action that runs because of it says `No approval needed: the person always allows "Payments" in Dinely.`
- So does whoever reviews the task: the step of an action that ran, or may have, because of it says `Always allowed: “Payments” in Dinely` on its second line, in Activity, on the task card and in the history export, so a payment on a standing answer does not read like one the person allowed just then. The step the person answered with Always allow has no such line.

### Other controls

- Read-only mode, per device or for everything.
- A per-task app allow-list (M1), such as "this task may only use Dinely"; typing into any other app is Ask.
- Emergency stop: a global shortcut (Option-Command-Period on macOS, Ctrl+Alt+Period elsewhere) and Stop all in the menu bar. It ends every lease and cancels every pending action.
- Credentials never reach the model. The gateway refuses `phone_type` into password and one-time-code fields (`blocked_by_policy`, rule: credentials) and tells the agent to call `phone_handoff` (M1). From M2 the client can fill the value from the OS keychain, and the agent only learns that it was filled.
- Notifications from blocked apps are never listed, and digit codes in notifications are masked by default.
- `phone_files` reaches only the phone's `Pinky/` folder. Writing to the phone is Ask, and read-only mode turns it off.
- An agent's first connection needs approval and a scope.

### Threat model

| Threat | Mitigation |
| --- | --- |
| Prompt injection through screen text, notifications or messages | Screen text is labeled untrusted; commits need approval; blocked apps stay invisible |
| A compromised agent forging consent | Consent happens only in the client UI, never as a tool argument; agent tokens reach tool methods only |
| An agent with shell access as the same user, calling adb or the gateway socket | Approval and policy methods accept only the in-process UI, or a client that proves user presence (Touch ID, Windows Hello). Against adb, run the agent in its own sandbox without access to adb or Pinky's socket, or attach the phones to another machine and use the remote API |
| Exfiltration through actions that are not commits (search boxes, autosaved drafts, links) | The per-task app allow-list; typing outside it is Ask |
| A reused approval or a changed screen | Single-use ids bound to the arguments; re-check before acting; `stale` |
| Another local process or a web page calling the gateway | User-only Unix socket or named pipe; Streamable HTTP only on 127.0.0.1, with the `Origin` check the MCP spec requires; per-agent tokens |
| Exposure through remote access | Off by default; Tailscale or mutual TLS with keys bound to each client |
| Sensitive data in history | Local storage, redaction, retention limits, wipe, optional encryption |
| Approval fatigue | "Allow for this task", "Always allow" per rule and app, clear intent text, fewer prompts for known flows |
| A standing Always allow used by a prompt-injected agent | Exemptions hold for one rule in one app and never lift a deny, read-only mode, an agent's scope or a task's app list; Settings › Safety lists them and takes them back; "Ask before every action" suspends them |

## Architecture

Pinky is one Go program that runs in three modes, with exactly one gateway per user. The gateway holds every device connection; the desktop client, the MCP shim and remote consoles all talk to it.

| Mode | Command | Runs |
| --- | --- | --- |
| Desktop app | `pinky` | Gateway and MyGo client in one process, with a menu bar icon; attaches to a running daemon instead of starting a second gateway |
| Headless gateway | `pinky daemon` | Gateway only, for a Mac mini, a NAS, a Linux box or CI |
| MCP shim | `pinky mcp` | The stdio server agents launch; it forwards to the running gateway, or starts the desktop app in tray mode (the daemon when no desktop session exists) |

(Diagram: see the design doc at https://claude.ai/artifact/1tETTimeUQMqvjvQf5ALsn)

A lock on the local socket keeps a second gateway from starting. Nothing under `internal/` imports MyGo, so the gateway runs without any UI.

### Action pipeline

1. **Lease and scope.** The call must come from the session that holds the device, for an app inside its scope.
2. **Policy.** Rules and the commit detector decide Allow, Ask or Deny.
3. **Approval.** For Ask, the approval center holds the call until the person decides or the timeout denies.
4. **Execute.** The driver performs the action on the long-lived device session.
5. **Settle.** The gateway waits up to 3 s for the screen to stop changing: encoded frames shrink to idle size and accessibility events go quiet.
6. **Verify and observe.** A fresh screen is read and diffed against the last one.
7. **Record.** The step, its before and after screenshots and any approval go to the history store.

### Observation pipeline

The driver's accessibility tree is normalized into the screen format, refs are assigned, OCR fills regions the tree misses, and the result is diffed against the previous screen.

### Video pipeline

- **Source:** H.264 from scrcpy on Android; HEVC over RTP from the CoreDevice display service on iOS 27+.
- **To clients:** the gateway forwards encoded packets, and each client decodes for display.
- **Recorder:** packets are remuxed into MP4 segments without decoding.
- **Screenshots and OCR:** the gateway decodes on demand from the last key frame, or asks the device for a fresh one (scrcpy's reset-video message).
- **Settle detector:** reads encoded frame sizes, so it decodes nothing.

### APIs

- **Local:** a user-only Unix socket (a named pipe on Windows) carries JSON-RPC for control and a binary stream for video and events.
- **Remote:** the same protocol over WebSocket, behind Tailscale or mutual TLS.
- **MCP:** stdio through the shim, or Streamable HTTP on 127.0.0.1, each with a per-agent token. Agent tokens reach tool methods only; approval, policy and lease methods accept the in-process UI or a client that proves user presence.

## Device drivers

Both drivers implement one Go interface (observe, tap, swipe, type, key, launch, list apps, files, stream, screenshot), so the gateway never branches on the platform. Each device keeps one long-lived session that carries video and input together.

### Android

- **Transport:** a Go client for the adb server protocol. It prefers the adb of an installed Android SDK, so it never fights Android Studio over the server, and bundles `adb` only when none exists. An adb server restart counts as the device going offline.
- **Screen and input:** Pinky bundles one version of [scrcpy](https://github.com/Genymobile/scrcpy)'s server, pushes it over adb and speaks its protocol directly: `key=value` options at start, control messages after. Upgrades are deliberate, because the server rejects a client of another version.
- **Background mode:** the server's virtual display options (`new_display`, `vd_destroy_content`) and its start-app control message let the agent work off the main screen. Some apps refuse secondary displays; the driver then falls back to the main display and says so.
- **Tree and events:** a Kotlin companion app runs an accessibility service that streams the tree, window changes, toasts and notifications, and sets text with `ACTION_SET_TEXT` (CJK works and the clipboard is untouched). It is on only while an agent works on the phone: on as a task starts, off a few seconds after it ends or the person takes over, or after a minute without the agent's calls, since banking apps refuse to run next to an accessibility service from outside an app store. Without it the driver falls back to `uiautomator dump` and keyboard or paste input. Its design follows [mobilerun-portal](https://github.com/droidrun/mobilerun-portal); no code is copied.
- **Takeover:** the driver reads physical touches with `getevent` on the touchscreen node, where injected input never appears. In background mode, touches on the main display do not pause the agent.
- **Files:** adb push and pull, limited to the phone's `Pinky/` folder.
- **Vendor limits:** Xiaomi needs "USB debugging (Security settings)" for input, and some ROMs turn off ADB after a while; setup checks detect both and explain the fix.

### iOS

Three backends sit behind the same interface; the gateway picks the best one the device allows.

| Backend | iOS | On the phone | Computer | Screen and tree | Role |
| --- | --- | --- | --- | --- | --- |
| CoreDevice (Device Hub protocol) via go-ios | 27+, Developer Mode on | Nothing | macOS, Windows, Linux (Windows and Linux to verify in step 0) | HEVC over RTP; element tree to verify in step 0 | Default |
| WebDriverAgent via go-ios | Older versions too | Signed WebDriverAgent | Xcode signs on a Mac; elsewhere a certificate. A free Apple ID's signature lasts 7 days, a paid developer account's a year | MJPEG; XCTest snapshot with frames | Compatibility, and frames when needed |
| iPhone Mirroring | 18+, with macOS 15+ | Nothing | macOS only | Window capture; OCR only | Zero-setup fallback |

- [go-ios](https://github.com/danielpaulus/go-ios) starts CoreDevice display streams (`ios/display`, added 2026-09-04) and injects touches through the universal HID service (`ios/hid`, added 2026-09-15). Both need iOS 27+ and the developer disk image. Touches apply only while a stream runs, so Pinky keeps the stream open for the whole session.
- **The element tree is the largest iOS risk.** go-ios reads elements by moving the accessibility inspector's focus; each has a spoken label but no role or parent. Its notes say that on a real iPhone 13 with iOS 18.4.1, per-element frames and hit-testing did not work; they worked only on the simulator. Step 0 tests iOS 27. If frames stay out of reach, OCR places labels on the frame, and WebDriverAgent supplies frames where it is installed.
- **Host prerequisites:** iOS 17+ traffic goes through a tunnel that may need administrator rights (`wintun` on Windows), and go-ios talks to the host's usbmuxd (the `usbmuxd` package on Linux; Apple Devices or iTunes on Windows). Setup checks cover each.
- **Gaps to fill:** keyboard HID for text, a way to set the iPhone clipboard for CJK without an app, the foreground app's bundle id, and pairing over Wi-Fi without a first USB connection. [idevice](https://github.com/jkcoxson/idevice) (Rust, MIT) is the reference.
- **Keys:** `back` is a left-edge swipe; `recents` returns `unsupported`.

### Locked phones

A phone that locks mid-task stops the agent, so Pinky avoids the lock where it can and lets the person unlock from the computer where it cannot. Nothing here bypasses the lock screen: after a reboot, the first unlock always happens on the phone.

| Approach | Platform | Phone stays untouched | Notes |
| --- | --- | --- | --- |
| Work on a virtual display | Android 13+ | Yes | scrcpy creates its displays with the always-unlocked flag ([source](https://github.com/Genymobile/scrcpy/blob/master/server/src/main/java/com/genymobile/scrcpy/video/NewDisplayCapture.java)), so apps there should keep running while the phone itself stays locked. Step 0 verifies it; if it holds, background mode moves to M1 |
| Stay awake while plugged in, screen off | Android | Yes | scrcpy's stay-awake and screen-off options ([docs](https://github.com/Genymobile/scrcpy/blob/master/doc/device.md)); the phone never reaches the lock screen while connected and charging |
| iPhone Mirroring backend | iOS 18+ with macOS 15+ | Yes | Apple keeps the iPhone locked during mirroring, and the person authenticates on the Mac ([Apple](https://support.apple.com/120421)) |
| Unlock from the computer | Android; iOS if step 0 shows touches reach the passcode screen | Yes | The PIN is saved in the OS keychain and entered by the gateway after Touch ID or Windows Hello. Agents never see it, only the client UI can trigger it, and every unlock is logged |
| Extend Unlock with the computer as a trusted Bluetooth device | Android | After one unlock | Keeps the phone unlocked for up to 4 hours at a time ([Google](https://support.google.com/android/answer/9075927)) |
| No lock screen | Any | Yes | Only for a spare phone used just for automation |

When the phone is locked anyway, actions return `device_locked`; the agent calls `phone_handoff`, and the client shows the locked-phone screen.

### Screen model and OCR

Both platforms produce the same screen format. OCR runs on the computer: Vision on macOS; Windows.Media.Ocr on Windows, which reads only languages whose language pack is installed (setup checks it); and a PaddleOCR model through ONNX Runtime on Linux, all loaded without cgo.

## Desktop client on MyGo

The client is built with [MyGo](https://github.com/egoist/mygo)'s native UI: Go views drawn on the GPU (Metal, Direct3D 11, OpenGL), with no webview. That suits an app that runs all day; MyGo's own counter benchmark is a 9 MB binary using 32 MB idle ([benchmarks](https://mygo.egoist.dev/benchmarks)), and step 0 measures Pinky's footprint.

| Need | macOS and Windows | Linux |
| --- | --- | --- |
| Approval panel that never takes focus | Non-activating floating panel; topmost tool window | Kept-above utility window; a layer-shell overlay on Wayland where the compositor allows it, else a notification |
| Menu bar or tray | Status item; notification area icon | AppIndicator |
| Emergency stop | Global shortcut | Key grab on X11; the XDG GlobalShortcuts portal on Wayland |
| Approval alerts | Notifications; a click opens the panel | The same, through the desktop's notification service |
| Mirror input | Pointer, wheel, keys and input-method compositions in custom elements | The same |
| Shipping | App bundle and DMG with notarization; signed Windows installer; signed delta updates | .deb, PKGBUILD and an install script |
| End-to-end tests | The automation plugin and its `mygo-automation` CLI, which an agent can drive | The same |

### Video surface

1. **Now:** FFmpeg's libavcodec, an LGPL build of a pinned version that Pinky ships, loads at run time through purego (a cgo-free way to call C libraries). It decodes with hardware acceleration: VideoToolbox, D3D11VA or VA-API. Each frame is read back from the GPU, converted to RGBA and passed to `Bitmap.Update`, which reuses the texture for same-size frames and takes an `*image.RGBA` without a copy. That is three steps per frame (read back, convert, upload), and step 0 measures all three. MyGo's author recommends this path today ([#973](https://github.com/egoist/mygo/issues/973)).
2. **Later:** zero-copy. The author plans texture import together with platform decoders ([#789](https://github.com/egoist/mygo/issues/789)); Pinky is a natural first user and will help build it.

Device grids use lower-resolution streams.

### The mirror element

- Draws the frame bitmap, then the overlay with `ui.Painter`: tap ripples, swipe trails, the target box and its ref label.
- Maps pointer down, move and up to touches in device coordinates; the wheel becomes scroll swipes; right click is Back and middle click is Home.
- Sends key presses as keys and committed input-method text as text, so Chinese, Japanese and Korean input works without a phone keyboard.
- Any input from the person during an agent task triggers takeover.

### What we ask upstream

1. Bitmap updates in NV12 or I420, converted to RGB in the shader: about 60% less upload and no CPU color conversion.
2. External texture import: CVPixelBuffer and IOSurface to Metal, D3D11 shared handles, DMA-BUF to OpenGL ([#973](https://github.com/egoist/mygo/issues/973)).
3. Action buttons on notifications, so a person can answer an approval without opening the panel.
4. The small input gaps tracked in [#1180](https://github.com/egoist/mygo/issues/1180).

## Data and storage

Everything lives in one SQLite database and a media folder in the user data directory; nothing leaves the machine unless the person exports it or turns on remote access. Policy rules live only in the rules file.

| Table | Holds |
| --- | --- |
| `devices` | Platform, model, OS version, backend, pairing record reference, last seen |
| `agents` | Name, client, token hash, scope, when the person approved it |
| `sessions` | Agent, devices leased, start and end, how it ended |
| `tasks` | Session, device, task name, start and end, outcome, step and approval counts |
| `steps` | Task, index, tool, arguments (JSON), intent, status, timing, before and after media, screen text |
| `approvals` | Step, matched rule, decision, note, time waited |
| `media` | Kind (screenshot, video segment, report), path, size, hash |

- **Media:** a WebP screenshot before and after each step, and MP4 segments remuxed from the device stream without re-encoding.
- **Retention:** one button wipes everything (M1); default limits of 30 days for video and 180 days for steps come with recording (M2). Blocked apps are never recorded.
- **Encryption:** optional, with the key kept in the OS keychain.
- **Export:** a single-file HTML report and JSON. iPhone video is HEVC, which browsers play unreliably, so the export transcodes it to H.264 with FFmpeg's hardware encoder, or links the MP4 when that fails.

## Tech stack and licenses

Pinky is Go end to end, with Kotlin only inside the Android companion; every dependency is permissive or, for FFmpeg, dynamically loaded LGPL.

| Layer | Choice | License | Why |
| --- | --- | --- | --- |
| Language | Go 1.27 | BSD-3-Clause | One language for gateway, drivers and UI; cross-compiles without cgo |
| Desktop UI | [MyGo](https://github.com/egoist/mygo) native UI | MIT | Light, native, cgo-free; tray, panels and updates built in |
| MCP | [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) | Apache-2.0 (older parts MIT) | Official SDK, version 1.8 |
| Android transport | adb server protocol (own code) and `adb` | Apache-2.0 | Shares the SDK's adb server with Android Studio |
| Android screen and input | [scrcpy](https://github.com/Genymobile/scrcpy) server, one pinned version | Apache-2.0 | Fast H.264 stream, control channel, virtual displays |
| Android companion | Kotlin accessibility service (own code) | Apache-2.0 | Fast tree, events and text input |
| iOS | [go-ios](https://github.com/danielpaulus/go-ios) | MIT | Pairing, tunnels, disk images, display stream, HID, accessibility, WebDriverAgent |
| iOS reference | [idevice](https://github.com/jkcoxson/idevice) | MIT | Reference for services go-ios lacks |
| Video decode | FFmpeg libavcodec, pinned, through purego | LGPL-2.1, loaded at run time | Hardware decoding on all three platforms |
| OCR | Vision, Windows.Media.Ocr, PaddleOCR on ONNX Runtime | OS, Apache-2.0, MIT | Local and free |
| Storage | SQLite through modernc.org/sqlite | Public domain, BSD-3-Clause | Pure Go, so the headless gateway needs no UI framework |
| Keychain | The OS keychain through a cgo-free library, to be chosen | To check | Credentials and the history key |

Pinky itself is proposed as Apache-2.0 for its patent grant. No code is copied from AGPL projects (Poki, and the Sonic device-farm platform) or FSL ones (mobilecli).

## Repository layout

One Go module holds everything except the Kotlin companion; packages follow the architecture's parts.

```text
cmd/pinky/              entry point: app, daemon and mcp modes
internal/gateway/         sessions, leases, action and observation pipelines
internal/policy/          rules, risk tiers, commit detection
internal/approval/        approval center, timeouts, MCP progress
internal/history/         SQLite store, media, export
internal/mcp/             MCP server and stdio shim
internal/api/             local socket and remote API (JSON-RPC, WebSocket)
internal/screen/          screen model, refs, diffs, OCR
internal/video/           stream demux, FFmpeg decoder, recorder, settle detector
internal/driver/android/  adb client, scrcpy session, companion protocol
internal/driver/ios/      go-ios backends: coredevice, wda, mirroring
ui/                       MyGo views: live session, approvals, history, setup
android/companion/        Kotlin accessibility service
docs/                     design, MCP reference, skill file
```

## Risks and mitigations

The biggest risks sit on iOS (Apple's private protocol and the element tree) and in MyGo's youth; each has a fallback.

| Risk | Mitigation |
| --- | --- |
| Apple changes the CoreDevice protocol, which is not public | Pin go-ios; test every iOS release; keep the WebDriverAgent backend |
| Real iPhones may not expose element frames over CoreDevice | Step 0 pass or fail; OCR placement; WebDriverAgent where installed |
| No install-free path below iOS 27 | WebDriverAgent with a signing wizard; check whether turning on Developer Mode needs one USB connection |
| MyGo is young: version 0.x, fast-moving, mostly one maintainer | Pin versions; keep the UI a thin layer over the gateway; contribute upstream; work directly with the author |
| The video path costs CPU until zero-copy lands | Measure in step 0; NV12 uploads upstream; lower-resolution streams in grids |
| HEVC hardware decoding on Windows varies by GPU and driver | FFmpeg D3D11VA with software fallback; lower resolution when needed |
| scrcpy's server protocol changes between versions | Pin one server version; upgrade deliberately, with tests |
| Android vendor limits and app detection of ADB or accessibility | Setup checks explain fixes; accessibility on only during tasks; finance apps blocked by default |
| Platforms ban automation in their terms | A clear notice at first run; per-app rate limits; no attempt to disguise automation |
| An agent with shell access bypasses Pinky | Documented; sandbox guidance; remote isolation mode |
| Packaging and signing for three platforms | MyGo's tooling builds bundles, installers, .deb and PKGBUILD; CI builds all three |

## Open questions

These need a decision before or during step 0.

- [ ] License: Apache-2.0 as proposed, or MIT?
- [ ] Does go-ios pair, stream and tap on Windows and Linux without a Mac for the first pairing?
- [ ] Can a real iPhone on iOS 27 give element frames over CoreDevice?
- [ ] With nothing installed, how can Pinky set the iPhone clipboard and read the foreground app?
- [ ] Can Developer Mode be turned on for iOS 27 without a USB connection?
- [ ] Is a 60 s approval timeout right, or should the default be longer?
- [ ] Telemetry: none at all, or opt-in crash reports only?
- [ ] Distribute the companion app through GitHub releases only, or also F-Droid and Google Play?
- [ ] Should "Allow for this task" also expire after a set time?
- [ ] When does a HarmonyOS driver matter enough to schedule?

## References

Pages read for this design, as of 2026-11-06.

- AndroMeld: [site](https://andromeld.catchingnow.com/), [help center](https://andromeld.catchingnow.com/help.html)
- Apple Device Hub: [WWDC26 session](https://developer.apple.com/videos/play/wwdc2026/260/), [managing Device Hub access](https://support.apple.com/en-ph/guide/personal-safety/ips37dbab2a1/web), [Michael Tsai's notes](https://mjtsai.com/blog/2026/06/25/xcode-27s-device-hub/)
- MyGo: [repository](https://github.com/egoist/mygo), [video docs](https://github.com/egoist/mygo/blob/main/docs/ui/video.md), [#973 texture interop](https://github.com/egoist/mygo/issues/973), [#789 video in native UI](https://github.com/egoist/mygo/issues/789), [#1180 input gaps](https://github.com/egoist/mygo/issues/1180), [benchmarks](https://mygo.egoist.dev/benchmarks)
- iOS: [go-ios](https://github.com/danielpaulus/go-ios) ([display](https://github.com/danielpaulus/go-ios/tree/main/ios/display), [hid](https://github.com/danielpaulus/go-ios/tree/main/ios/hid), [accessibility notes](https://github.com/danielpaulus/go-ios/blob/main/ios/accessibility/ax_audit.go)), [idevice](https://github.com/jkcoxson/idevice), [device-hub-ios](https://github.com/JaviSoto/device-hub-ios)
- Android: [scrcpy](https://github.com/Genymobile/scrcpy), [server options](https://github.com/Genymobile/scrcpy/blob/master/server/src/main/java/com/genymobile/scrcpy/Options.java), [control messages](https://github.com/Genymobile/scrcpy/blob/master/server/src/main/java/com/genymobile/scrcpy/control/ControlMessage.java), [mobilerun-portal](https://github.com/droidrun/mobilerun-portal)
- MCP: [Go SDK](https://github.com/modelcontextprotocol/go-sdk), [MCP Apps](https://blog.modelcontextprotocol.io/posts/2026-01-26-mcp-apps/)
- Web engines, on why the client avoids webview video: [WebKitGTK 2.44 notes](https://webkitgtk.org/2024/03/27/webkigit-2.44.html), [an Edge HEVC decoding report](https://techcommunity.microsoft.com/discussions/edgeinsiderdiscussions/hevch265-hardware-decoding-fails-in-edge-on-intel-graphics/4553940)
- Other projects are linked in the appendix.

## Appendix: projects reviewed

As of 2026-11-06: what Pinky borrows from each project and what each lacks for this job.

| Project | Shape | What we borrow | What it lacks |
| --- | --- | --- | --- |
| [AndroMeld](https://andromeld.catchingnow.com/) | Closed-source Mac app with a helper APK: mirroring, apps in their own windows, Finder mount, MCP | Compact element list with stable refs; each action returns the new screen; task name shown to the user; phone storage as a local folder; accessibility on only during tasks | macOS and Android only; per its 1.10 docs, MCP drives only the main screen; consent is relayed by the agent through a `confirm` parameter |
| [Escrcpy](https://github.com/viarotel-org/escrcpy) | Electron GUI for scrcpy | Multi-device windows, wireless discovery, key mapping | No approvals or audit; some features in a paid private repo |
| [mobile-mcp](https://github.com/mobile-next/mobile-mcp) / [mobilecli](https://github.com/mobile-next/mobilecli) | MCP server and Go CLI for iOS and Android | Long-lived daemon; element refs; WebView and Safari inspection | No human console; iOS needs a signed agent; mobilecli is FSL, not OSI open source |
| [Maestro](https://github.com/mobile-dev-inc/Maestro) | YAML test flows, MCP, desktop Studio | Agent exploration turned into deterministic flows | Built for testing; no physical iPhones |
| [Midscene](https://github.com/web-infra-dev/midscene) | Vision agent framework for Android, iOS and HarmonyOS | Per-step HTML report | A test framework with its own model, not a gateway |
| [phone-harness](https://github.com/phone-harness/phone-harness) | CLI; the agent writes Python against helpers | Code mode: one script, many steps | iPhone only through Mirroring and OCR; no UI |
| [mirroir-mcp](https://github.com/jfarcand/mirroir-mcp) | Drives iPhone Mirroring on macOS | Read-only by default; a permissions file and blocked apps; compiled skills replay without a model | macOS only; OCR only |
| [Poki](https://github.com/JosssphZhou/poki) | iOS over CoreDevice, nothing on the phone | Remote access over Tailscale; learned routes | Verified on one device; a new media session per action with a 1 s wait; no approval UI; AGPL |
| [iPhone-use](https://github.com/zhongerxin/iPhone-use) | iOS through WebDriverAgent, Python MCP | Reliable XCTest input; live screen in the chat | Signing required; slow trees in heavy apps; analytics on by default |
| [Open-AutoGLM](https://github.com/zai-org/Open-AutoGLM) | Phone agent model and framework | Sensitive-action confirmation and human takeover callbacks | Ships its own model; not a gateway for outside agents |
| [Xcode Device Hub](https://developer.apple.com/videos/play/wwdc2026/260/) | Apple's remote view and control of iOS 27+ devices | Proof that the protocol backs a supported Apple feature; Safety Check lists sessions | Mac only; `devicectl` has no screen control |
