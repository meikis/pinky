# Companion protocol

The Android companion app (`android/companion`, package `app.pinky.companion`) serves this protocol from its accessibility service. The Go client is `internal/driver/android/companion`. This page is the reference for both; [companion.md](companion.md) covers the app, the client and test results.

Protocol version: **1**. Additive changes (new commands, new fields, new event kinds) keep the number; incompatible changes bump it. Clients read `protocol` from `ping`, and `features` for additions they must not assume an older app has: an older app ignores parameters it does not know, so a `set_text` with a `display` would write on the main display there.

Changes since the first version, all additive:

| When | Change | Feature |
| --- | --- | --- |
| 2026-11-07, round 3 | `display` on `set_text`, `events` and `wait_idle`; `display` on every event; `features` in `ping`; `tree` fails clearly for a display it cannot read | `displays` (Android 11+) |
| 2026-11-07, round 3 | `config` values are kept until the service is turned off, across restarts | none: an older app forgets them when its service restarts; `ping` shows the current values |
| 2026-11-07, round 3 | `region` on windows; `tree` walks an input method window that covers the screen like a sheet even without `ime` | none: an older app leaves `region` out, and a client that reads it treats its absence as "the bounds" |

## Transport

- The service listens on the abstract Unix socket `@pinky_companion`, only while it is bound (from the moment Android connects the service until it is turned off). The development build (`app.pinky.companion.dev`, what builds from the source carry, which can be installed beside a release) listens on `@pinky_companion_dev` instead.
- The computer reaches it with `adb forward tcp:0 localabstract:pinky_companion` and connects to `127.0.0.1:<port>`. adb accepts the TCP connection even when nothing listens on the phone and then closes it, so a dead service shows up as EOF on the first read.
- Any app can connect to an abstract socket, so the server checks the peer's credentials and accepts only uid 0 (root), uid 2000 (shell, which is what adbd uses) and the app's own uid. Anyone else gets one line, `{"ok":false,"error":{"code":"forbidden","message":"only adb may connect"}}`, and is disconnected.
- Any number of connections may be open. Each has its own reader thread; requests on one connection run one at a time, in order. Everything that reads the accessibility tree runs on a single worker thread inside the app, so trees from two connections never interleave.

## Framing

- One JSON object per line, UTF-8, terminated by `\n`. Strings may hold any Unicode, including CJK and emoji; the app escapes control characters, U+2028/U+2029 and lone surrogates.
- Request: `{"id": <number or string>, "cmd": "<command>", ...parameters}`.
- Success: `{"id": <same id>, "ok": true, "result": {...}}`.
- Failure: `{"id": <same id>, "ok": false, "error": {"code": "<code>", "message": "<text>"}}`.
- Event (only after `events` on that connection): `{"event": "<kind>", "time": <ms>, ...}`, with no `id`.
- Times are the phone's wall clock in Unix milliseconds. `ping` returns the phone's current time so a client can estimate the clock skew (the Go client does, and converts event times to the host's clock).
- Rectangles are `[left, top, right, bottom]` in screen pixels of the display's current rotation.
- Boolean fields on nodes and windows are sent only when true; absent means false.

## Displays

Every command that reads or writes the screen takes `display`, an Android display id; 0, the main display, is the default. Other displays are for background work: the driver opens a virtual display with scrcpy and starts an app there (`internal/driver/android/background.go`), so an agent can work in it while the person uses the main screen.

- **Android 11 and later** (`ping` lists the feature `displays`): any display Android opens to accessibility services, which includes the main display and public virtual displays such as scrcpy's. Android leaves out private virtual displays and overlay displays (the developer option "Simulate secondary displays"); asking for one gives `unsupported`.
- **Android 10**: the main display only. Any other display gives `unsupported`: Android 10 gives accessibility services the main display's windows only, and its events do not say which display they came from.
- A display that does not exist, or no longer does, gives `not_found`. A negative id gives `bad_request`.
- Bounds, and the `x` and `y` of a `set_text` target, are pixels of that display.
- Input focus: the phone's input focus follows the last touch, so it is on one display at a time, and the other displays have no focused window. Their views keep their focus, though, so `set_text` on a display that was not touched last uses the focused view of its top-most application window (see `set_text`).

## Commands

### `ping`

```json
{"id":1,"cmd":"ping"}
```

| Field | Meaning |
| --- | --- |
| `protocol` | Protocol version (1) |
| `features` | Additions this app supports on this phone, beyond protocol 1 as it first shipped: `displays` (Android 11+) means `tree`, `set_text`, `events` and `wait_idle` take displays other than 0, and events name their display. Absent in apps built before 2026-11-07. |
| `version`, `version_code` | The app's version name (Pinky's version, from `cmd/pinky/mygo.json`) and code (the number of commits up to the one built); see [companion.md](companion.md#versions-and-signing) |
| `sdk`, `release` | Android API level and version string |
| `manufacturer`, `model` | `Build.MANUFACTURER`, `Build.MODEL` |
| `time`, `uptime_ms` | Phone wall clock (Unix ms) and uptime |
| `displays` | Ids of displays that have accessibility windows (always `[0]` before Android 11) |
| `connections` | Open connections, including this one |
| `include_not_important`, `idle_disable_ms` | Current `config` values |

### `tree`

All windows on one display and their node trees.

```json
{"id":2,"cmd":"tree","display":0}
```

| Parameter | Default | Meaning |
| --- | --- | --- |
| `display` | 0 | Display id; see [Displays](#displays). |
| `ime` | false | Walk the keyboard window's nodes. Off by default: keys are noise for agents. The window is always listed. An input method window that covers the screen like a sheet rather than a keyboard is walked anyway: one that reaches into the top third of the display and whose region covers at least half of it, such as Gboard's stylus introduction over a scrim. Android then reports the app's nodes under it as not visible, so without it the tree would be empty. |
| `bars` | false | Walk the status and navigation bars: system windows that span one screen edge and are at most 12% thick. They are always listed, so clients can tell what covers the app. |
| `invisible` | false | Keep nodes that are not visible to the user (marked `"invisible": true`). By default they are skipped with their subtrees, as `uiautomator dump` does. |
| `max_nodes` | 20000 | Stop after this many nodes and set `truncated`. |
| `timeout_ms` | 10000 | Fail with `timeout` after this long. |

Result:

| Field | Meaning |
| --- | --- |
| `display` | `{id, width, height, density_dpi, rotation}`; width and height in pixels for the current rotation, rotation in degrees |
| `locked` | The keyguard is showing. It belongs to the phone and covers the main display only, whatever `display` was asked for. |
| `secure_locked` | The keyguard needs a PIN, pattern or password |
| `windows` | Windows sorted top-most first (by layer) |
| `nodes`, `truncated` | Nodes written, and whether `max_nodes` cut the walk |
| `time` | Phone time when the walk finished |
| `took_ms`, `windows_ms` | Time spent on the request, and on listing windows |

Window:

| Field | Meaning |
| --- | --- |
| `id`, `layer` | Accessibility window id and z-order (higher is on top) |
| `type` | `application`, `input_method`, `system`, `accessibility_overlay`, `split_screen_divider`, `magnification_overlay` or `unknown` |
| `title` | Window title, if any (an activity's label, "Navigation bar", ...) |
| `bounds` | Window rectangle: the bounds of its touchable region |
| `region` | The touchable region as rectangles, `[[left, top, right, bottom], ...]`, when it is more than the one rectangle of `bounds` (Android 13+; left out above 32 rectangles). Gboard's stylus toolbar, for example, has `bounds` `[0,896,1080,2400]` but `region` `[[21,896,169,1586],[0,2337,1080,2400]]`. Android hides from the user only the nodes of lower windows that the region covers, so a client that cuts elements under higher windows should cut by `region` when it is there. |
| `active`, `focused`, `pip` | Active window, input focus, picture-in-picture |
| `package`, `app` | Package of the root node, and its display name. Names load in the background, so the first tree after an app opens may lack `app`. |
| `root` | The root node, absent when the window was not walked |
| `root_ms`, `walk_ms`, `nodes` | Time to fetch the root (a round trip to the app), time to walk it, and its node count |

Node:

| Field | Meaning |
| --- | --- |
| `class` | View class name, such as `android.widget.Button` |
| `text` | Text. Never sent for password fields, and not sent while a field shows its hint. |
| `desc` | Content description |
| `hint` | Hint text (Android 8+) |
| `id` | Resource id, `package:id/name` |
| `state` | State description, such as "On" or "50%" (Android 11+) |
| `role` | Role description the app set (Compose, `AccessibilityNodeInfoCompat`), such as "Tab"; may be localized |
| `bounds` | Rectangle on the screen, not clipped |
| `clickable`, `long_clickable`, `checkable`, `checked`, `editable`, `scrollable`, `focusable`, `focused`, `selected`, `password`, `heading`, `disabled` | States |
| `node` | A handle for `set_text`: stable while the view lives (it is `AccessibilityNodeInfo.hashCode()`, built from the view and window ids) |
| `children` | Child nodes in order |

By default the tree holds only views that are important for accessibility (see `config`).

### `set_text`

Writes text into an editable node with `ACTION_SET_TEXT`. The text goes straight into the field: any script works, and the clipboard and the keyboard are not involved.

```json
{"id":3,"cmd":"set_text","text":"你好，Pinky","target":{"id":"com.example:id/note"}}
```

| Parameter | Meaning |
| --- | --- |
| `text` | Required. The new text, or the text to append. |
| `target` | Which node; see below. Absent or `{}` means the focused input. |
| `display` | The display whose windows the target is looked up in; default 0. See [Displays](#displays). |
| `append` | Keep the current text and add to it. Fails with `unsupported` on password fields, whose text cannot be read. |
| `submit` | Focus the node first, then run its IME action (`ACTION_IME_ENTER`, Android 11+). On Android 10 the result has `"submitted": false` and the client should press Enter itself. |
| `timeout_ms` | Default 10000. |

Targets, in order of precedence:

| Target | Picks |
| --- | --- |
| `{"focused": true}` | The node with input focus on the display: the phone's input focus when it is there; otherwise the focused view of the display's focused window, or, on a display that was not touched last, of its top-most application window. A dialog without a focused field gives `not_found`, never the field behind it. |
| `{"node": 4242, "window": 41}` | The node with this handle from a `tree` result; `window` narrows the search |
| `{"x": 540, "y": 1296, "id": "..."}` | The smallest editable node under the point, top-most window first; among those, one with the resource id `id` if given |
| `{"id": "com.example:id/note"}` | The first visible editable node with this resource id, top-most window first |

Result: `window` and `node` (the handle of the node written), `text` (the field's text afterwards, read back from the app), `verified` (whether it equals the requested text; an app may change it, for example with a length limit), and `submitted` when `submit` was set. `text` and `verified` are left out for password fields.

### `events`

Subscribes the connection to events. The reply comes first; events follow on the same connection until it closes. A new `events` request replaces the subscription; `"kinds": []` ends it.

```json
{"id":4,"cmd":"events","kinds":["window","toast","notification"]}
```

| Parameter | Default | Meaning |
| --- | --- | --- |
| `kinds` | `window`, `toast`, `notification` | Event kinds |
| `display` | all displays | Only events of this display. Notifications belong to the phone and always pass; so does `dropped`. See [Displays](#displays). |

The result is `{"kinds": [...], "display": <id, when given>, "time": <ms>}`.

Every event has `event` (the kind), `time` (when it happened on the phone), `package` when known, and `display`: the display it came from, or -1 when that is not known. Android 13 and later report it for every event. On Android 11 and 12 the event's window tells, so a window that has only just opened can be -1; on Android 10 only the main display's windows are known. Toast events name no window: their display is the one Android reports, else the display where the app last opened a window, else, for an app that opened none since the service started, the display its windows were on then. Notifications are always -1: they belong to the phone, not to a display. Content changes are folded per display.

| Kind | When | Extra fields |
| --- | --- | --- |
| `window` | A new activity, dialog, pane or popup (`TYPE_WINDOW_STATE_CHANGED`) | `class`, `title`, `window` |
| `windows` | A window was added, removed, moved, retitled or focused (`TYPE_WINDOWS_CHANGED`) | `window`, `changes`: any of `added`, `removed`, `title`, `bounds`, `layer`, `active`, `focused`, `accessibility_focused`, `parent`, `children`, `pip` |
| `content` | Content changed, scrolled or text changed. Bursts are folded: at most one event per 100 ms. | `count`: events folded into this one |
| `toast` | A toast appeared, including toasts SystemUI draws for apps targeting Android 11+ | `text`. SystemUI's toasts (Android 12+) also report the app's name; when the event has several texts, the one equal to the app's name is dropped. |
| `notification` | A notification was posted or alerted again | `title`, `text` (falls back to the big text, then the ticker), `category` |
| `dropped` | The connection fell behind and the app dropped events (its queue holds 2048); `display` is -1 | `count` |

Notifications can carry personal data. The app forwards what Android reports; filtering (for example of blocked apps) is the gateway's job.

### `wait_idle`

Blocks until no change event (window state, windows, content, scroll, text) arrived for `quiet_ms`, counted from the request at the earliest, or until `timeout_ms` passed. The Go client's `WaitSettled` (the `driver.Settler` interface) uses it with 300 ms.

```json
{"id":5,"cmd":"wait_idle","quiet_ms":300,"timeout_ms":3000}
```

| Parameter | Default | Range |
| --- | --- | --- |
| `quiet_ms` | 300 | 10 to 10000 |
| `timeout_ms` | 3000 | 0 to 30000 |
| `display` | all displays | Count only changes on this display, and those whose display is not known |

Result: `settled` (false when the timeout came first), `waited_ms`, and `last_change_ms_ago` when any change was seen since the service started.

### `config`

Changes the service's settings until the service is turned off. The app saves them, so they survive the service restarting while it stays on: a reboot, an app update, a `uiautomator dump`. When the service is turned off, by the person in Android Settings, by Pinky or by its own idle watchdog, the app forgets them and the next start begins from the defaults. It tells the two apart by reading `enabled_accessibility_services` when Android unbinds it: still listed means a restart.

| Parameter | Default | Meaning |
| --- | --- | --- |
| `include_not_important` | false | Include views that are not important for accessibility (layout containers), as `uiautomator dump` does. Before Android 13 the app cannot clear its node cache, so the change takes full effect at the next window change. |
| `idle_disable_ms` | 60000 (180000 in builds until 2026-11-08) | The service turns itself off (`disableSelf`) after this long without any client: no connection open and none opened, closed or used meanwhile. Time the phone spends asleep counts, and so does time across a restart of the service (a `uiautomator dump` rebinds it). 0 keeps it on until it is turned off, also after a reboot or an update. Pinky's Install sent 0 until 2026-11-08; Pinky now sends nothing, as it turns the service on only while an agent works. |

The result holds the current values.

## Error codes

| Code | Meaning |
| --- | --- |
| `bad_request` | Malformed JSON, a missing parameter, an unknown event kind or a negative display id |
| `unknown_command` | No such `cmd` |
| `not_found` | No node matches the target, the node is gone, or the display does not exist |
| `not_editable` | The target node is not editable |
| `disabled` | The target node is disabled |
| `unsupported` | The request cannot work here: append to a password field, a display other than 0 before Android 11, or a display Android keeps from accessibility services |
| `action_failed` | The app refused `ACTION_SET_TEXT` |
| `timeout` | The request took longer than `timeout_ms` |
| `forbidden` | The peer is not adb, root or the app itself |
| `internal` | An unexpected exception; the message has its text |

The Go client maps `not_found` to `driver.ErrNotFound` and `unsupported` to `driver.ErrUnsupported`.

## Service lifecycle

- Pinky turns the service on by adding only its own component, `app.pinky.companion/.PinkyAccessibilityService`, to `enabled_accessibility_services`, and off by removing it again; see [companion.md](companion.md#turning-the-service-on-and-off).
- When the service connects it binds the socket (retrying for up to 5 seconds if a previous instance still holds the name); when Android unbinds it, the socket closes and every connection ends.
- After `idle_disable_ms` without any client the service turns itself off, unless `config` set it to 0. The count goes on when Android only rebinds the service, and it is checked again every time it does. This is a backstop for a gateway that crashed or lost the phone while the service was on for a task: while an agent works, Pinky keeps an event subscription open, and it turns the service off itself when the agent stops.
- `uiautomator dump` (and any other UiAutomation client that does not ask to keep services) makes Android unbind every accessibility service for as long as it runs, so the companion restarts with an empty cache around each dump. Do not mix the two.
