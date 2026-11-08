package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	mg "github.com/egoist/mygo/ui"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/gateway"
	"github.com/DIYgod/pinky/internal/secret"
	"github.com/DIYgod/pinky/ui/fakeapi"
)

// --- unlocking from this computer ---

// TestSavePINThenUnlock saves a PIN on the locked phone's card, unlocks
// with it, and removes it in Settings › Phones. The PIN never shows.
func TestSavePINThenUnlock(t *testing.T) {
	ta := newTestApp(t, fakeapi.Locked)
	store := ta.opt.Secrets.(*secret.Memory)
	ta.waitFor(t, "the unlock card", func() bool {
		return len(ta.needs) == 1 && ta.needs[0].Kind == gateway.NeedUnlock && ta.pin.ready
	})
	tt := mg.NewTester(ta.view, 1280, 820)
	label, _ := unlockWords()
	if hasText(tt, label) {
		t.Fatalf("an unlock button shows with no PIN saved: %q", tt.Texts())
	}
	if !hasText(tt, "Save the phone's PIN to unlock it from this computer, or unlock it on the phone.") {
		t.Errorf("the card does not say how to unlock from here: %q", tt.Texts())
	}
	if err := tt.Click("Save the PIN for Pixel 9"); err != nil {
		t.Fatal(err)
	}
	tt.Type("2468")
	tt.Frame()
	if hasText(tt, "2468") {
		t.Errorf("the PIN shows as it is typed: %q", tt.Texts())
	}
	if err := tt.Click("Save PIN"); err != nil {
		t.Fatal(err)
	}
	ta.waitFor(t, "the PIN to be saved", func() bool { return ta.pin.saved["pixel-9"] })
	if pin, err := store.Reveal(context.Background(), secret.PINAccount("pixel-9"), "test"); err != nil || pin != "2468" {
		t.Fatalf("the keychain has %q, %v", pin, err)
	}
	if ta.pin.value != "" {
		t.Errorf("the client still holds the PIN it saved")
	}
	tt.Frame()
	if hasText(tt, "2468") {
		t.Errorf("the PIN shows after saving: %q", tt.Texts())
	}
	if err := tt.Click(label); err != nil {
		t.Fatalf("no unlock button once the PIN is saved: %v: %q", err, tt.Texts())
	}
	ta.waitFor(t, "the phone to unlock", func() bool {
		return len(ta.needs) == 0 && ta.devices[0].Status != gateway.DeviceLocked
	})

	ta.page, ta.set.sec = pageSettings, secPhones
	tt.Frame()
	if !hasText(tt, "PIN saved") {
		t.Fatalf("Settings › Phones does not show the saved PIN: %q", tt.Texts())
	}
	if err := tt.Click("Remove the PIN for Pixel 9"); err != nil {
		t.Fatal(err)
	}
	ta.waitFor(t, "the PIN to be removed", func() bool { return !ta.pin.saved["pixel-9"] })
	if store.Has(secret.PINAccount("pixel-9")) {
		t.Error("the keychain still has the PIN")
	}
}

// TestShortPINIsKept refuses a PIN too short to be one, keeping what was
// typed so it can be fixed.
func TestShortPINIsKept(t *testing.T) {
	ta := newTestApp(t, fakeapi.Locked)
	ta.waitFor(t, "the keychain", func() bool { return ta.pin.ready && len(ta.needs) == 1 })
	tt := mg.NewTester(ta.view, 1280, 820)
	if err := tt.Click("Save the PIN for Pixel 9"); err != nil {
		t.Fatal(err)
	}
	tt.Type("12")
	if err := tt.Click("Save PIN"); err != nil {
		t.Fatal(err)
	}
	if !hasText(tt, "at least 4 characters") || ta.pin.value != "12" {
		t.Errorf("a short PIN: %q, value %q", tt.Texts(), ta.pin.value)
	}
	if ta.opt.Secrets.Has(secret.PINAccount("pixel-9")) {
		t.Error("a short PIN was saved")
	}
}

// unsupportedStore is a system without a keychain Pinky can use.
type unsupportedStore struct{}

func (unsupportedStore) Supported() bool           { return false }
func (unsupportedStore) Has(string) bool           { return false }
func (unsupportedStore) Save(string, string) error { return secret.ErrUnsupported }
func (unsupportedStore) Delete(string) error       { return secret.ErrUnsupported }
func (unsupportedStore) Reveal(context.Context, string, string) (string, error) {
	return "", secret.ErrUnsupported
}

// TestUnlockNotAvailable says so where the system has no keychain, on the
// card and in Settings.
func TestUnlockNotAvailable(t *testing.T) {
	ta := newTestApp(t, fakeapi.Locked)
	ta.opt.Secrets = unsupportedStore{}
	ta.waitFor(t, "the keychain probe", func() bool { return ta.pin.ready && len(ta.needs) == 1 })
	if ta.pin.supported {
		t.Fatal("an unsupported store counts as a keychain")
	}
	tt := mg.NewTester(ta.view, 1280, 820)
	if !hasText(tt, notAvailableHere+" Unlock it on the phone.") {
		t.Errorf("the card: %q", tt.Texts())
	}
	if label, _ := unlockWords(); hasText(tt, label) || hasText(tt, "Save PIN…") {
		t.Errorf("the card offers to unlock from here: %q", tt.Texts())
	}
	ta.page, ta.set.sec = pageSettings, secSafety
	tt.Frame()
	if !hasText(tt, notAvailableHere) {
		t.Errorf("Settings › Safety: %q", tt.Texts())
	}
}

func TestUnlockErrors(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{nil, ""},
		{secret.ErrCancelled, ""},
		{errors.Join(errors.New("entered"), driver.ErrLocked), "The saved PIN didn't unlock Mi 10 Pro."},
		{errors.Join(errors.New("no pad"), driver.ErrPINNotEntered), "Pinky couldn't get to Mi 10 Pro's passcode screen, so it entered nothing."},
		{secret.ErrNotFound, "No PIN is saved for Mi 10 Pro."},
		{secret.ErrUnsupported, notAvailableHere},
		{gateway.ErrNoUnlock, "Unlocking from this computer isn't available."},
		{driver.ErrOffline, "Mi 10 Pro is offline."},
		{errors.New("adb: broken pipe"), "Couldn't unlock Mi 10 Pro: adb: broken pipe"},
	} {
		got := unlockError(c.err, "Mi 10 Pro")
		if (c.want == "") != (got == "") || !strings.HasPrefix(got, c.want) {
			t.Errorf("unlockError(%v) = %q, want %q…", c.err, got, c.want)
		}
	}
}

// --- pairing ---

// TestPairByQRCode shows the gateway's QR code, drawn module by module,
// waits for the phone and then shows it.
func TestPairByQRCode(t *testing.T) {
	ta := newTestAppWith(t, fakeapi.Options{Scenario: fakeapi.OnePhone, Speed: 50, PairDelay: -1})
	ta.sync()
	ta.page = pageAddPhone
	ta.add.reset(ta.app)
	ta.add.step = "qr"
	tt := mg.NewTester(ta.view, 1280, 820)
	ta.waitFor(t, "the QR code", func() bool { tt.Frame(); return ta.add.pair.code != nil })
	tt.Frame()
	st := ta.add.pair
	if !strings.HasPrefix(st.text, "WIFI:T:ADB;S:") || !hasText(tt, "Waiting for the phone to scan the code…") {
		t.Fatalf("QR text %q, page %q", st.text, tt.Texts())
	}
	r, ok := tt.Find("QR code to scan in Wireless debugging › Pair device with QR code")
	if !ok {
		t.Fatal("no QR code on the page")
	}
	// Every module's middle has the module's color.
	img := tt.Image()
	scale := float32(img.Rect.Dx()) / 1280
	ox, oy, cell := qrGeometry(r, scale, st.code.Size)
	bad := 0
	for y := 0; y < st.code.Size; y++ {
		for x := 0; x < st.code.Size; x++ {
			px := int((ox + (float32(x)+0.5)*cell) * scale)
			py := int((oy + (float32(y)+0.5)*cell) * scale)
			if dark := lum(img, px, py) < 128; dark != st.code.Black(x, y) {
				bad++
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d of %d modules drawn wrong", bad, st.code.Size*st.code.Size)
	}
	ta.api.ScanPairing()
	ta.waitFor(t, "the paired phone", func() bool {
		return ta.page == pagePhones && strings.HasPrefix(ta.sel, "pixel-8a-wifi-")
	})
	if ta.add.pair.cancel != nil {
		t.Error("the pairing still runs")
	}
}

func lum(img *image.RGBA, x, y int) int {
	i := img.PixOffset(x, y)
	return (int(img.Pix[i]) + int(img.Pix[i+1]) + int(img.Pix[i+2])) / 3
}

// TestPairWithCode pairs by pairing code, after a wrong address and a
// wrong code, each said in plain words.
func TestPairWithCode(t *testing.T) {
	ta := newTestAppWith(t, fakeapi.Options{Scenario: fakeapi.OnePhone, Speed: 50, PairDelay: -1})
	ta.sync()
	ta.page = pageAddPhone
	ta.add.reset(ta.app)
	ta.add.step = "qr"
	tt := mg.NewTester(ta.view, 1280, 820)
	if err := tt.Click("Pair with a code instead"); err != nil {
		t.Fatal(err)
	}
	ta.sync()
	if ta.add.pair.cancel != nil {
		t.Error("the QR pairing still runs behind the code form")
	}
	st := &ta.add.pair
	try := func(addr, code, want string) {
		t.Helper()
		st.addr, st.pin = addr, code
		tt.Frame()
		if err := tt.Click("Pair"); err != nil {
			t.Fatal(err)
		}
		ta.waitFor(t, "an answer", func() bool { return !st.formBusy })
		if st.formErr != want {
			t.Errorf("%s / %s: %q, want %q", addr, code, st.formErr, want)
		}
	}
	try("the phone", "123456", "Enter the IP address and port the phone shows, like 192.168.1.20:37123.")
	try("192.168.1.20:37123", "000000", "The pairing code didn't match. Use the code the phone shows now; it changes each time you open Pair device with pairing code.")
	try("192.168.1.20:1", "123456", "Couldn't reach the phone at that address. Check the IP address and port under Pair device with pairing code; they change each time it opens.")
	tt.Frame()
	if !hasText(tt, "Couldn't reach the phone at that address.") && !hasText(tt, st.formErr) {
		t.Errorf("the error does not show: %q", tt.Texts())
	}
	st.addr, st.pin = "192.168.1.20:37123", "123456"
	tt.Frame()
	if err := tt.Click("Pair"); err != nil {
		t.Fatal(err)
	}
	ta.waitFor(t, "the paired phone", func() bool {
		return ta.page == pagePhones && strings.HasPrefix(ta.sel, "pixel-8a-wifi-")
	})
}

// TestPairingWithoutPairer says so on a gateway that cannot pair.
func TestPairingWithoutPairer(t *testing.T) {
	ta := newTestApp(t, fakeapi.OnePhone)
	ta.app.api = struct{ gateway.API }{ta.api} // the fake can pair; hide that behind gateway.API
	ta.sync()
	ta.page = pageAddPhone
	ta.add.reset(ta.app)
	ta.add.step = "qr"
	tt := mg.NewTester(ta.view, 1280, 820)
	if !hasText(tt, "This gateway can't pair phones over Wi-Fi.") || hasText(tt, "Pair with a code instead") {
		t.Errorf("no-pairing page: %q", tt.Texts())
	}
}

// --- the companion app ---

// TestCompanionInstallCard offers the companion app on the phone's page,
// installs it on Install with the phone's prompt in mind, and goes away.
func TestCompanionInstallCard(t *testing.T) {
	ta := newTestApp(t, fakeapi.Companion)
	ta.sync()
	tt := mg.NewTester(ta.view, 1280, 820)
	ta.waitFor(t, "the companion state", func() bool { tt.Frame(); return ta.comp.state["pixel-9"] == driver.CompanionMissing })
	tt.Frame()
	for _, want := range []string{"Install Pinky Companion", "faster and more steadily", "type without using its clipboard", "If the phone asks, allow the install.", "Not now"} {
		if !hasText(tt, want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	if err := tt.Click("Install Pinky Companion on Pixel 9"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !hasText(tt, "If the phone asks, allow the install.") || ta.comp.installing["pixel-9"].IsZero() {
		t.Errorf("installing: %q", tt.Texts())
	}
	ta.waitFor(t, "the install", func() bool { return ta.comp.state["pixel-9"] == driver.CompanionReady })
	if got := ta.api.Installs(); len(got) != 1 || got[0] != "pixel-9" {
		t.Errorf("installs: %v", got)
	}
	tt.Frame()
	if hasText(tt, "Install Pinky Companion") {
		t.Errorf("the card stays after the install: %q", tt.Texts())
	}
	ta.page, ta.set.sec = pageSettings, secPhones
	tt.Frame()
	if !hasText(tt, "Pinky Companion turns on only while an agent works") {
		t.Errorf("Settings › Phones: %q", tt.Texts())
	}
}

func TestCompanionNotNow(t *testing.T) {
	ta := newTestApp(t, fakeapi.Companion)
	ta.sync()
	tt := mg.NewTester(ta.view, 1280, 820)
	ta.waitFor(t, "the card", func() bool { tt.Frame(); return hasText(tt, "Install Pinky Companion") })
	if err := tt.Click("Not now"); err != nil {
		t.Fatal(err)
	}
	if hasText(tt, "Install Pinky Companion") || len(ta.api.Installs()) != 0 {
		t.Errorf("after Not now: %q", tt.Texts())
	}
}

// --- background displays ---

// TestBackgroundSwitch turns background work off and on in Settings ›
// Phones, which the gateway keeps.
func TestBackgroundSwitch(t *testing.T) {
	ta := newTestApp(t, fakeapi.Background)
	ta.sync()
	ta.page, ta.set.sec = pageSettings, secPhones
	tt := mg.NewTester(ta.view, 1280, 820)
	if !hasText(tt, "Work in the background") || !ta.bg.on["pixel-9"] {
		t.Fatalf("no switch, or off: %q", tt.Texts())
	}
	if err := tt.Click("Work in the background"); err != nil {
		t.Fatal(err)
	}
	ta.waitFor(t, "background off", func() bool { return !ta.api.Background("pixel-9") && !ta.bg.busy["pixel-9"] })
	if ta.bg.on["pixel-9"] {
		t.Error("the switch stays on")
	}
}

// TestAgentScreenInMirror shows the agent's display while it works in the
// background, takes no input there, and switches to the phone's screen.
func TestAgentScreenInMirror(t *testing.T) {
	ta := newTestApp(t, fakeapi.Background)
	ta.waitFor(t, "the task", func() bool { return len(ta.tasks) == 1 && running(ta.tasks[0].State) })
	tt := mg.NewTester(ta.view, 1280, 820)
	if _, ok := tt.Find("Claude Code's screen on Pixel 9, live"); !ok {
		t.Fatalf("the mirror does not show the agent's screen: %q", tt.Texts())
	}
	if !hasText(tt, "Phone's screen") {
		t.Errorf("no switch to the phone's screen: %q", tt.Texts())
	}
	if m := ta.mirrors["agent:pixel-9"]; m == nil || !m.agent {
		t.Fatal("no agent mirror")
	}
	if err := tt.Click("Phone's screen"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("Pixel 9 screen, live"); !ok {
		t.Errorf("the phone's own screen does not show: %q", tt.Texts())
	}
	tt.Frame()
	if ta.mirrors["agent:pixel-9"] != nil {
		t.Error("the agent's stream stays open while the phone's screen shows")
	}
	// Without an agent's display open, the phone's screen shows and no
	// switch.
	ta.bg.phone["pixel-9"] = ""
	tt.Frame()
	am := ta.mirrors["agent:pixel-9"]
	am.state, am.err, am.retry = "failed", errors.Join(errors.New("background displays"), driver.ErrNotFound), time.Now().Add(time.Hour)
	tt.Frame()
	if _, ok := tt.Find("Pixel 9 screen, live"); !ok || hasText(tt, "Phone's screen") {
		t.Errorf("no agent display: %q", tt.Texts())
	}
	// The person taking over uses the phone's own screen.
	delete(ta.mirrors, "agent:pixel-9")
	tt.Frame()
	if _, ok := tt.Find("Claude Code's screen on Pixel 9, live"); !ok {
		t.Fatalf("the agent's screen does not come back: %q", tt.Texts())
	}
	if err := ta.api.TakeOver("pixel-9"); err != nil {
		t.Fatal(err)
	}
	ta.waitFor(t, "control", func() bool { return ta.devices[0].Status == gateway.DeviceInControl })
	tt.Frame()
	if _, ok := tt.Find("Pixel 9 screen, live"); !ok || hasText(tt, "Phone's screen") {
		t.Errorf("in control, the mirror does not show the phone's screen: %q", tt.Texts())
	}
}

// --- recordings and export ---

// TestRecordingButtons offers the recording when the task has one, and
// the screenshot replay when it does not.
func TestRecordingButtons(t *testing.T) {
	ta := newTestApp(t, fakeapi.OnePhone)
	ta.page = pageActivity
	tt := mg.NewTester(ta.view, 1280, 820)
	ta.waitFor(t, "Activity", func() bool { tt.Frame(); return ta.act.loaded })
	ta.act.sel = "h-lunch"
	ta.waitFor(t, "the recording", func() bool { tt.Frame(); return hasText(tt, "Watch recording") })
	ta.act.sel = "h-gate" // recording paused for a blocked app
	ta.waitFor(t, "the replay", func() bool {
		tt.Frame()
		return hasText(tt, "Watch replay") && ta.rec.views["h-gate"] != nil && !ta.rec.views["h-gate"].loading
	})
	if hasText(tt, "Watch recording") {
		t.Errorf("a task without a recording offers one: %q", tt.Texts())
	}
}

// TestRecordingJumpsToSteps opens the player on a task's recording and
// jumps to a step's mark from the list; the fake is asked to play from
// there. Video stays off in this test.
func TestRecordingJumpsToSteps(t *testing.T) {
	ta := newTestApp(t, fakeapi.OnePhone)
	ta.page = pageActivity
	tt := mg.NewTester(ta.view, 1280, 820)
	ta.waitFor(t, "Activity", func() bool { tt.Frame(); return ta.act.loaded })
	ta.act.sel = "h-lunch"
	ta.waitFor(t, "the recording", func() bool { tt.Frame(); return hasText(tt, "Watch recording") })
	if err := tt.Click("Watch recording"); err != nil {
		t.Fatal(err)
	}
	pl := ta.rec.pl
	if pl == nil || pl.task != "h-lunch" {
		t.Fatal("no player")
	}
	tt.Frame()
	if !hasText(tt, "Jump to a step") || !hasText(tt, "You allowed: pay $12.80") {
		t.Fatalf("no step marks: %q", tt.Texts())
	}
	var mark gateway.RecordingMark
	for _, m := range pl.rec.Marks {
		if strings.HasPrefix(m.Text, "You allowed") {
			mark = m
		}
	}
	if mark.At <= 0 {
		t.Fatalf("no step to jump to after the start: %+v", pl.rec.Marks)
	}
	if err := tt.Click("Jump to " + clock(mark.At) + ": You allowed: pay $12.80"); err != nil {
		t.Fatal(err)
	}
	// Watch recording opened the recording at the start on a goroutine of
	// its own, which may ask the fake after the jump's does.
	ta.waitFor(t, "the jump", func() bool { return slices.Contains(ta.api.RecordingOpens(), mark.At) })
	if pl.markAt(pl.pos) < 0 || pl.rec.Marks[pl.markAt(pl.pos)].At != mark.At {
		t.Errorf("the current step after the jump: pos %v", pl.pos)
	}
	ta.page = pagePhones
	tt.Frame()
	if ta.rec.pl != nil {
		t.Error("the player stays open after leaving Activity")
	}
}

// TestExport exports one task and every task through the gateway, and
// writes its own report for a gateway that cannot.
func TestExport(t *testing.T) {
	ta := newTestApp(t, fakeapi.OnePhone)
	var revealed []string
	ta.reveal = func(p string) { revealed = append(revealed, p) }
	ta.page = pageActivity
	tt := mg.NewTester(ta.view, 1280, 820)
	ta.waitFor(t, "Activity", func() bool { tt.Frame(); return ta.act.loaded })
	if err := tt.Click("Export all"); err != nil {
		t.Fatal(err)
	}
	ta.waitFor(t, "the export", func() bool { return len(revealed) == 1 })
	ex := ta.api.Exports()
	if len(ex) != 1 || len(ex[0].IDs) != 0 || filepath.Base(revealed[0]) != "pinky-history.html" {
		t.Fatalf("exports %+v, revealed %v", ex, revealed)
	}
	if !strings.HasPrefix(filepath.Base(ex[0].Dir), "Pinky activity ") {
		t.Errorf("export folder %q", ex[0].Dir)
	}
	ta.exportHistory([]string{"h-lunch"}, "Reorder last week's lunch")
	ta.waitFor(t, "the task's export", func() bool { return len(revealed) == 2 })
	if ex := ta.api.Exports(); len(ex) != 2 || len(ex[1].IDs) != 1 || ex[1].IDs[0] != "h-lunch" {
		t.Errorf("exports %+v", ex)
	}

	// A gateway without ExportHistory, or with the stub, gets the
	// client's own report.
	for _, api := range []gateway.API{struct{ gateway.API }{ta.api}, unsupportedExport{ta.api}} {
		ta.app.api = api
		ta.exportHistory([]string{"h-lunch"}, "lunch")
		n := len(revealed)
		ta.waitFor(t, "the client's export", func() bool { return len(revealed) == n+1 })
		data, err := os.ReadFile(revealed[n])
		if err != nil || filepath.Base(revealed[n]) != "report.html" || !strings.Contains(string(data), "Reorder last week&#39;s lunch") {
			t.Errorf("client report %s: %v\n%s", revealed[n], err, data)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(revealed[n]), "tasks.json")); err != nil {
			t.Error(err)
		}
	}
}

type unsupportedExport struct{ gateway.API }

func (unsupportedExport) ExportHistory(context.Context, []string, string) (string, error) {
	return "", errors.Join(errors.New("export"), driver.ErrUnsupported)
}

// TestRetentionAndRecordingSettings passes Keep activity and Record to the
// gateway at start and on each change, and saves them.
func TestRetentionAndRecordingSettings(t *testing.T) {
	ta := newTestApp(t, fakeapi.OnePhone)
	var saved []Settings
	ta.opt.OnSettings = func(_, s Settings) { saved = append(saved, s) }
	ta.settings.KeepDays, ta.settings.Record = 90, false
	ta.applySettings()
	ta.waitFor(t, "the settings at start", func() bool { days, rec := ta.api.Retention(); return days == 90 && !rec })
	ta.page, ta.set.sec = pageSettings, secPrivacy
	tt := mg.NewTester(ta.view, 1280, 820)
	if err := tt.Click("Record the screen during tasks"); err != nil {
		t.Fatal(err)
	}
	ta.waitFor(t, "recording on", func() bool { _, rec := ta.api.Retention(); return rec })
	if len(saved) == 0 || !saved[len(saved)-1].Record {
		t.Errorf("not saved: %+v", saved)
	}
	if got := keepChoices(14); got[0] != 14 || keepLabel(0) != "Forever" || keepLabel(365) != "1 year" {
		t.Errorf("keep choices %v", got)
	}
}

// --- remote agents ---

func TestRemoteAgents(t *testing.T) {
	ta := newTestApp(t, fakeapi.OnePhone)
	ta.opt.AgentToken = "tok-5ecret"
	var saved []Settings
	ta.opt.OnSettings = func(_, s Settings) { saved = append(saved, s) }
	ta.sync()
	ta.page, ta.set.sec = pageSettings, secAgents
	tt := mg.NewTester(ta.view, 1280, 820)
	if hasText(tt, "http://127.0.0.1:7465/mcp") {
		t.Errorf("the URL shows while remote agents are off: %q", tt.Texts())
	}
	if err := tt.Click("Let agents connect over HTTP"); err != nil {
		t.Fatal(err)
	}
	if !ta.settings.RemoteAgents || len(saved) == 0 || !saved[len(saved)-1].RemoteAgents {
		t.Fatal("the switch did not turn remote agents on")
	}
	tt.Frame()
	if !hasText(tt, "http://127.0.0.1:7465/mcp") || hasText(tt, "tok-5ecret") {
		t.Errorf("URL, or the token in the open: %q", tt.Texts())
	}
	if err := tt.Click("Copy the url"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != "http://127.0.0.1:7465/mcp" {
		t.Errorf("copied %q", tt.Clipboard())
	}
	if err := tt.Click("Copy the command"); err != nil {
		t.Fatal(err)
	}
	want := `claude mcp add --transport http pinky http://127.0.0.1:7465/mcp --header "Authorization: Bearer tok-5ecret"`
	if tt.Clipboard() != want {
		t.Errorf("copied %q, want %q", tt.Clipboard(), want)
	}
	if err := tt.Click("Show the agent token"); err != nil {
		t.Fatal(err)
	}
	if !hasText(tt, "tok-5ecret") {
		t.Errorf("Show does not show the token: %q", tt.Texts())
	}
	// A new address, checked, with a warning when others can reach it.
	if err := tt.Click("Change the address"); err != nil {
		t.Fatal(err)
	}
	ta.remote.addr = "not an address"
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	if !hasText(tt, "Enter an address and port, like 127.0.0.1:7465.") || ta.settings.RemoteAddr != DefaultRemoteAddr {
		t.Errorf("a bad address: %q", tt.Texts())
	}
	ta.remote.addr = "0.0.0.0:7465"
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	if ta.settings.RemoteAddr != "0.0.0.0:7465" || !hasText(tt, "Other computers on your network can reach Pinky") {
		t.Errorf("a public address: %q", tt.Texts())
	}
}

func TestCheckRemoteAddr(t *testing.T) {
	for _, c := range []struct {
		addr   string
		ok     bool
		others bool
	}{
		{"127.0.0.1:7465", true, false}, {"localhost:80", true, false}, {"[::1]:7465", true, false},
		{"0.0.0.0:7465", true, true}, {"192.168.1.5:7465", true, true}, {":7465", true, true},
		{"127.0.0.1", false, false}, {"127.0.0.1:0", false, false}, {"example.com:7465", false, false},
	} {
		msg, others := checkRemoteAddr(c.addr)
		if (msg == "") != c.ok || others != c.others {
			t.Errorf("%s: %q, others %v", c.addr, msg, others)
		}
	}
}

// --- the Mac awake while tasks run ---

func TestAwakeWhileTasksRun(t *testing.T) {
	ta := newTestApp(t, fakeapi.OnePhone)
	var calls []bool
	ta.awake = func(on bool) { calls = append(calls, on) }
	ta.waitFor(t, "the task", func() bool { return ta.anyRunning() })
	if len(calls) != 1 || !calls[0] {
		t.Fatalf("awake calls %v while a task runs", calls)
	}
	if err := ta.api.StopAll(); err != nil {
		t.Fatal(err)
	}
	ta.waitFor(t, "the task to stop", func() bool { return !ta.anyRunning() })
	if len(calls) != 2 || calls[1] {
		t.Errorf("awake calls %v after the task", calls)
	}
}

// TestLockCardOffersPIN tells the person about unlocking from this
// computer when a phone is locked and nothing needs them: Save PIN…
// without a saved PIN, Unlock once one is saved, and Not now hides it.
func TestLockCardOffersPIN(t *testing.T) {
	ta := newTestApp(t, fakeapi.OnePhone)
	ta.waitFor(t, "the phones and the keychain probe", func() bool { return ta.pin.ready && len(ta.devices) > 0 })
	d := ta.devices[0]
	d.Status, d.Features.Unlock = gateway.DeviceLocked, true
	ta.devices[0], ta.needs, ta.sel, ta.page = d, nil, d.Info.ID, pagePhones
	tt := mg.NewTester(ta.view, 1280, 820)
	tt.Frame()
	for _, want := range []string{d.Info.Name + " is locked", "Save PIN…", "Not now"} {
		if !hasText(tt, want) {
			t.Errorf("no %q on a locked phone's page: %q", want, tt.Texts())
		}
	}
	if err := ta.opt.Secrets.Save(secret.PINAccount(d.Info.ID), "1234"); err != nil {
		t.Fatal(err)
	}
	ta.pin.saved[d.Info.ID] = true
	tt.Frame()
	if label, _ := unlockWords(); !hasText(tt, label) || hasText(tt, "Save PIN…") {
		t.Errorf("with a saved PIN the card should offer to unlock: %q", tt.Texts())
	}
	ta.pin.saved[d.Info.ID] = false
	ta.pin.later[d.Info.ID] = true
	tt.Frame()
	if hasText(tt, "Save PIN…") || hasText(tt, "Not now") {
		t.Errorf("Not now did not hide the card: %q", tt.Texts())
	}
}

// TestRefusedPINIsNotRetried keeps Unlock from trying a PIN the phone
// refused again, which could lock the person out, until the phone is
// unlocked on it or the PIN changes; a PIN that was never entered may be
// tried again.
func TestRefusedPINIsNotRetried(t *testing.T) {
	ta := newTestApp(t, fakeapi.OnePhone)
	ta.waitFor(t, "the phones and the keychain probe", func() bool { return ta.pin.ready && len(ta.devices) > 0 })
	d := ta.devices[0]
	d.Status, d.Features.Unlock = gateway.DeviceLocked, true
	ta.devices[0], ta.needs, ta.sel, ta.page = d, nil, d.Info.ID, pagePhones
	ta.pin.saved[d.Info.ID] = true
	if !ta.canUnlockHere(d.Info.ID) {
		t.Fatal("a locked phone with a saved PIN cannot be unlocked")
	}
	ta.pin.wrong[d.Info.ID] = true
	tt := mg.NewTester(ta.view, 1280, 820)
	tt.Frame()
	if label, _ := unlockWords(); hasText(tt, label) {
		t.Errorf("Unlock offered again after the phone refused the PIN: %q", tt.Texts())
	}
	if !hasText(tt, "The saved PIN didn't unlock it, so Pinky won't try it again. Unlock it on the phone. If its PIN changed, update it in Settings › Phones.") {
		t.Errorf("the card does not say why: %q", tt.Texts())
	}
	if msg := unlockError(fmt.Errorf("unlock: the passcode pad did not appear: %w", driver.ErrPINNotEntered), "iPhone"); !strings.Contains(msg, "entered nothing") {
		t.Errorf("a PIN that was never entered: %q", msg)
	}
}

// TestPairingProgress follows the pairing on the status line: the phone
// found on the network, then paired and connecting.
func TestPairingProgress(t *testing.T) {
	ta := newTestAppWith(t, fakeapi.Options{Scenario: fakeapi.OnePhone, Speed: 50, PairDelay: -1})
	ta.sync()
	ta.page = pageAddPhone
	ta.add.reset(ta.app)
	ta.add.step = "qr"
	tt := mg.NewTester(ta.view, 1280, 820)
	ta.waitFor(t, "the QR code", func() bool { tt.Frame(); return ta.add.pair.code != nil })
	for _, c := range []struct {
		pr   driver.PairingProgress
		want string
	}{
		{driver.PairingProgress{}, "Waiting for the phone to scan the code…"},
		{driver.PairingProgress{Found: true}, "Found the phone. Pairing…"},
		{driver.PairingProgress{Found: true, Paired: true}, "Paired. Connecting to the phone…"},
	} {
		ta.api.SetPairingProgress(c.pr)
		tt.Frame()
		if !hasText(tt, c.want) {
			t.Errorf("%+v: %q", c.pr, tt.Texts())
		}
	}
}

// TestPairingStuck says, once a pairing has waited 30 seconds, what may be
// wrong and what to do next, and offers the pairing code.
func TestPairingStuck(t *testing.T) {
	ta := newTestApp(t, fakeapi.OnePhone)
	ta.page = pageAddPhone
	ta.add.reset(ta.app)
	ta.add.step = "qr"
	var pr driver.PairingProgress
	st := &ta.add.pair
	set := func(p driver.PairingProgress, ago time.Duration) {
		ta.stopPairing()
		st.state, st.since = "waiting", time.Now().Add(-ago)
		st.progress, st.foundAt = func() driver.PairingProgress { return pr }, time.Time{}
		pr = p
	}
	set(driver.PairingProgress{}, 0)
	tt := mg.NewTester(ta.view, 1280, 820)
	tt.Frame()
	if hasText(tt, "The phone hasn't shown up on the network yet") {
		t.Fatal("the note shows at once")
	}
	notAllowed := "Something on this computer, perhaps a firewall, keeps Pinky from looking for phones on the network."
	if runtime.GOOS == "darwin" {
		notAllowed = "Turn on Pinky under System Settings › Privacy & Security › Local Network, then try again."
	}
	for _, c := range []struct {
		pr          driver.PairingProgress
		title, text string
	}{
		{driver.PairingProgress{}, "The phone hasn't shown up on the network yet", "Some networks, such as guest Wi-Fi and many offices, keep devices from finding each other"},
		{driver.PairingProgress{NotAllowed: true, SearchErr: "mdns: not allowed"}, "Pinky isn't allowed to look for phones on this network", notAllowed},
		{driver.PairingProgress{SearchErr: "mdns: no network interface to search"}, "Pinky can't look for phones on this network", "(mdns: no network interface to search)"},
	} {
		set(c.pr, 31*time.Second)
		tt.Frame()
		if !hasText(tt, c.title) || !hasText(tt, c.text) {
			t.Errorf("%+v: %q", c.pr, tt.Texts())
		}
		// A button shows its text and its label.
		if n := countText(tt, "Pair with a code instead"); n != 2 {
			t.Errorf("%+v: the pairing code is offered %d times: %q", c.pr, n/2, tt.Texts())
		}
		if got := hasText(tt, "Open Local Network settings"); got != (c.pr.NotAllowed && runtime.GOOS == "darwin") {
			t.Errorf("%+v: settings button shown: %v", c.pr, got)
		}
	}
	// On a Mac the settings button opens Local Network.
	if runtime.GOOS == "darwin" {
		set(driver.PairingProgress{NotAllowed: true}, 31*time.Second)
		var opened string
		saved := openURL
		openURL = func(_ *mg.Context, u string, done func(error)) { opened = u; done(nil) }
		t.Cleanup(func() { openURL = saved })
		tt.Frame()
		if err := tt.Click("Open Local Network settings"); err != nil {
			t.Fatal(err)
		}
		if opened != localNetworkSettings {
			t.Errorf("opened %q", opened)
		}
	}
	// A phone that showed up gets a while to pair; the note then says so.
	set(driver.PairingProgress{Found: true}, 31*time.Second)
	tt.Frame()
	if hasText(tt, "The phone showed up, but pairing hasn't finished") {
		t.Error("the note shows as soon as the phone showed up")
	}
	st.foundAt = time.Now().Add(-16 * time.Second)
	tt.Frame()
	if !hasText(tt, "The phone showed up, but pairing hasn't finished") {
		t.Errorf("no note 16 s after the phone showed up: %q", tt.Texts())
	}
	// Paired phones are connecting: no note.
	set(driver.PairingProgress{Found: true, Paired: true}, 5*time.Minute)
	tt.Frame()
	if hasText(tt, "The phone showed up, but pairing hasn't finished") || countText(tt, "Pair with a code instead") != 2 {
		t.Errorf("a note while connecting: %q", tt.Texts())
	}
	// The note's button goes to the pairing-code form.
	set(driver.PairingProgress{}, 31*time.Second)
	tt.Frame()
	if err := tt.Click("Pair with a code instead"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !st.form || st.cancel != nil || !hasText(tt, "Pair with a code") || hasText(tt, "The phone hasn't shown up on the network yet") {
		t.Errorf("after the button: form %v, %q", st.form, tt.Texts())
	}
}

func countText(tt *mg.Tester, s string) int {
	n := 0
	for _, t := range tt.Texts() {
		if strings.Contains(t, s) {
			n++
		}
	}
	return n
}

// TestPairWithCodeConnectStep: the phone paired but did not show up on
// the network, so the form asks for the address Wireless debugging shows
// and connects there.
func TestPairWithCodeConnectStep(t *testing.T) {
	ta := newTestAppWith(t, fakeapi.Options{Scenario: fakeapi.OnePhone, Speed: 50, PairDelay: -1})
	ta.sync()
	ta.page = pageAddPhone
	ta.add.reset(ta.app)
	ta.add.step = "qr"
	tt := mg.NewTester(ta.view, 1280, 820)
	if err := tt.Click("Pair with a code instead"); err != nil {
		t.Fatal(err)
	}
	ta.sync()
	st := &ta.add.pair
	st.addr, st.pin = "192.168.1.20:2", "123456"
	tt.Frame()
	if err := tt.Click("Pair"); err != nil {
		t.Fatal(err)
	}
	ta.waitFor(t, "the connect step", func() bool { tt.Frame(); return st.connect && !st.formBusy })
	if !hasText(tt, "Connect to the phone") || !hasText(tt, "enter the IP address and port shown at the top") {
		t.Fatalf("connect step: %q", tt.Texts())
	}
	try := func(addr, want string) {
		t.Helper()
		st.connAddr = addr
		tt.Frame()
		if err := tt.Click("Connect"); err != nil {
			t.Fatal(err)
		}
		ta.waitFor(t, "an answer", func() bool { return !st.formBusy })
		if st.formErr != want {
			t.Errorf("%s: %q, want %q", addr, st.formErr, want)
		}
	}
	try("the phone", "Enter the IP address and port the phone shows, like 192.168.1.20:37123.")
	try("192.168.1.20:1", "Couldn't connect at that address. Use the IP address and port at the top of Wireless debugging, not the ones of the pairing dialog.")
	st.connAddr = "192.168.1.20:41234"
	tt.Frame()
	if err := tt.Click("Connect"); err != nil {
		t.Fatal(err)
	}
	ta.waitFor(t, "the connected phone", func() bool {
		return ta.page == pagePhones && strings.HasPrefix(ta.sel, "pixel-8a-wifi-")
	})
	if st.connect || st.form {
		t.Errorf("the form stayed: connect %v, form %v", st.connect, st.form)
	}
}
