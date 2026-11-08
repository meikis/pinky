# Recorded trees

Fixtures for `screen_recorded_test.go`: the companion app's `tree` results, taken from our Android 16 emulator (`pinky-api36`, Gboard 15.1, the companion app of release 2026.1107.1) as they came over the socket, only indented. Each shows Gboard's stylus mode in the Settings search, the case where an input method window covers the app:

- `real-android16-emulator-gboard-stylus-toolbar.json`: the stylus toolbar while results for "display" show. The keyboard's window spans the screen below y 896, but its touchable `region` is a strip on the left and one at the bottom; the results are visible and take taps.
- `real-android16-emulator-gboard-stylus-menu.json`: the toolbar's menu (More stylus options). The keyboard's window covers everything below the status bar; the app walks it unasked and sends none of the search page's nodes.
- `real-android16-emulator-gboard-stylus-sheet.json`: "Try out your stylus" (Try handwriting demo in that menu) over a scrim. The results show dimmed under it but take no taps.

To take them again: open Settings, tap the search field through the emulator console (`adb emu event mouse 540 243 0 1`, then `... 0 0`: the console's mouse arrives as a stylus, so Gboard switches to its stylus toolbar), type "display", then tap the toolbar's menu button and Try handwriting demo, reading the tree after each step.

Never take trees from someone's phone without them: they hold what is on the screen.

# The old public signer

`old-public-signer.der` is the certificate (public, no key) of `android/companion/pinky-companion-debug.keystore`, which signed every build from 2026-11-07 until the release key on 2026-11-08 and was committed with its password, so anyone could sign an update with it. `dumpsys package` names it `dd80ed25`; the tests check that `SignerName` gives the same, so that Pinky can tell those builds and replace them.
