package ui

import (
	"context"
	"crypto/sha256"
	hexenc "encoding/hex"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	mg "github.com/egoist/mygo/ui"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/update"
	"github.com/DIYgod/pinky/ui/fakeapi"
)

// fakeInstaller stands in for Pinky.app's installer: it records what it
// staged and how the swap was asked to start the new version.
type fakeInstaller struct {
	mu     sync.Mutex
	staged [][]byte
	swaps  []update.StartOptions
}

func (f *fakeInstaller) Stage(_ context.Context, file string, r update.Release) (update.Staged, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.staged = append(f.staged, data)
	f.mu.Unlock()
	return fakeStaged{f}, nil
}

type fakeStaged struct{ f *fakeInstaller }

func (s fakeStaged) Swap(opt update.StartOptions) error {
	s.f.mu.Lock()
	s.f.swaps = append(s.f.swaps, opt)
	s.f.mu.Unlock()
	return nil
}
func (fakeStaged) Discard() {}

func (f *fakeInstaller) swapped() []update.StartOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]update.StartOptions(nil), f.swaps...)
}

const testNotes = "- Faster mirroring\n- Pinky updates itself"

// feed serves latest.json for version and its zip.
func feed(t *testing.T, version string) (*httptest.Server, []byte) {
	t.Helper()
	asset := []byte("the zip of Pinky.app " + version)
	h := sha256.Sum256(asset)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest.json":
			fmt.Fprintf(w, `{"version": %q, "tag": "v%s", "date": "2026-11-07T10:00:00Z", "notes": %q, "assets": {%q: {"url": %q, "sha256": %q, "size": %d}}}`,
				version, version, testNotes, update.Platform(), srv.URL+"/Pinky.zip", hexenc.EncodeToString(h[:]), len(asset))
		case "/Pinky.zip":
			w.Write(asset)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, asset
}

type updateTest struct {
	*testApp
	u     *update.Updater
	in    *fakeInstaller
	quits int
}

// newUpdateTest is a client of version 2026.1107.1 with a feed that offers
// 2026.1107.2, and an installation that can replace itself.
func newUpdateTest(t *testing.T, s fakeapi.Scenario) *updateTest {
	t.Helper()
	srv, _ := feed(t, "2026.1107.2")
	ut := &updateTest{testApp: newTestApp(t, s), in: &fakeInstaller{}}
	dir := t.TempDir()
	ut.u = &update.Updater{
		Version: "2026.1107.1", Feed: srv.URL + "/latest.json",
		Location:  update.Location{Method: update.MethodSelf, App: filepath.Join(dir, "Pinky.app")},
		Installer: ut.in, Cache: filepath.Join(dir, "cache"), Downloads: filepath.Join(dir, "Downloads"),
		StatePath: filepath.Join(dir, "update.json"),
	}
	ut.opt.Updates, ut.opt.Version = ut.u, "2026.1107.1"
	ut.settings.AutoUpdate = true
	ut.upd.quit = func() { ut.quits++ }
	ut.upd.idleTime = func() time.Duration { return time.Hour }
	return ut
}

func (ut *updateTest) check(t *testing.T) {
	t.Helper()
	ut.checkUpdate(false)
	ut.waitFor(t, "the update check", func() bool { return !ut.upd.checking })
}

func TestUpdateCardThenUpdateNow(t *testing.T) {
	ut := newUpdateTest(t, fakeapi.Companion)
	ut.check(t)
	if ut.upd.rel == nil || ut.upd.rel.Version != "2026.1107.2" {
		t.Fatalf("no release after the check: %+v", ut.upd.rel)
	}
	tt := mg.NewTester(ut.view, 1280, 820)
	if !hasText(tt, "Pinky 2026.1107.2 is available") {
		t.Fatalf("no update card: %q", tt.Texts())
	}
	if err := tt.Click("What's new in Pinky 2026.1107.2"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !hasText(tt, "Faster mirroring") || !hasText(tt, "November 7, 2026") {
		t.Errorf("the release notes: %q", tt.Texts())
	}
	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Update now Pinky 2026.1107.2"); err != nil {
		t.Fatal(err)
	}
	ut.waitFor(t, "the restart", func() bool { return ut.quits > 0 })
	swaps := ut.in.swapped()
	if len(swaps) != 1 || swaps[0].Background || slices.Contains(swaps[0].Args, "--background") {
		t.Errorf("swaps %+v: Update now brings Pinky back in front", swaps)
	}
	if len(ut.in.staged) != 1 || string(ut.in.staged[0]) != "the zip of Pinky.app 2026.1107.2" {
		t.Errorf("staged %q", ut.in.staged)
	}
	tt.Frame()
	if !hasText(tt, "Restarting Pinky") {
		t.Errorf("the card while restarting: %q", tt.Texts())
	}
}

func TestUpdateNowAsksWhileAnAgentWorks(t *testing.T) {
	ut := newUpdateTest(t, fakeapi.OnePhone)
	ut.waitFor(t, "the agent's task", func() bool { return ut.anyRunning() })
	ut.check(t)
	tt := mg.NewTester(ut.view, 1280, 820)
	if err := tt.Click("Update now Pinky 2026.1107.2"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !hasText(tt, "Update Pinky now?") || !hasText(tt, "The agent task that runs now stops.") {
		t.Fatalf("no confirmation: %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if ut.upd.phase != updIdle || ut.quits != 0 {
		t.Errorf("Cancel went on: phase %v, quits %d", ut.upd.phase, ut.quits)
	}
}

func TestAutomaticUpdateWaitsUntilIdle(t *testing.T) {
	ut := newUpdateTest(t, fakeapi.OnePhone)
	ut.waitFor(t, "the agent's task", func() bool { return ut.anyRunning() })
	ut.check(t)
	ut.updateTick(time.Now())
	if ut.upd.phase != updIdle {
		t.Fatalf("installed while an agent works (phase %v)", ut.upd.phase)
	}

	ut2 := newUpdateTest(t, fakeapi.Companion)
	ut2.check(t)
	ut2.upd.lastInput = time.Now().Add(-time.Minute)
	ut2.updateTick(time.Now())
	if ut2.upd.phase != updIdle {
		t.Fatal("installed a minute after the person used Pinky")
	}
	ut2.settings.AutoUpdate = false
	ut2.upd.lastInput = time.Time{}
	ut2.updateTick(time.Now())
	if ut2.upd.phase != updIdle {
		t.Fatal("installed with automatic updates off")
	}
	ut2.settings.AutoUpdate = true
	ut2.updateTick(time.Now())
	ut2.waitFor(t, "the automatic update", func() bool { return ut2.quits > 0 })
	swaps := ut2.in.swapped()
	// No window was open: the new version starts in the menu bar, in the
	// background.
	if len(swaps) != 1 || !swaps[0].Background || !slices.Contains(swaps[0].Args, "--background") {
		t.Errorf("swaps %+v", swaps)
	}
}

// TestUpdateOnWindows: a Pinky that its installer installed on Windows
// updates itself with the release's installer, which runs quietly: the card
// offers Update now, and Pinky installs it by itself while idle.
func TestUpdateOnWindows(t *testing.T) {
	ut := newUpdateTest(t, fakeapi.Companion)
	setup := []byte("MZ the installer of Pinky 2026.1107.2")
	h := sha256.Sum256(setup)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest.json":
			fmt.Fprintf(w, `{"version": "2026.1107.2", "tag": "v2026.1107.2", "date": "2026-11-07T10:00:00Z", "notes": %q, "assets": {%q: {"url": %q, "sha256": %q, "size": %d}}}`,
				testNotes, update.Platform(), srv.URL+"/pinky-setup-2026.1107.2-amd64.exe", hexenc.EncodeToString(h[:]), len(setup))
		case "/pinky-setup-2026.1107.2-amd64.exe":
			w.Write(setup)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	ut.u.Feed = srv.URL + "/latest.json"
	ut.u.Location = update.Location{Method: update.MethodSelf, Setup: true,
		App: `C:\Users\x\AppData\Local\Programs\Pinky`, Exe: `C:\Users\x\AppData\Local\Programs\Pinky\Pinky.exe`}
	ut.check(t)
	tt := mg.NewTester(ut.view, 1280, 820)
	if !hasText(tt, "Update now") || hasText(tt, "Download and install") {
		t.Errorf("the card: %q", tt.Texts())
	}
	ut.updateTick(time.Now())
	ut.waitFor(t, "the automatic update", func() bool { return ut.quits > 0 })
	if len(ut.in.staged) != 1 || string(ut.in.staged[0]) != string(setup) {
		t.Errorf("staged %q", ut.in.staged)
	}
	if swaps := ut.in.swapped(); len(swaps) != 1 || !slices.Contains(swaps[0].Args, "--background") {
		t.Errorf("swaps %+v", swaps)
	}
}

func TestAutomaticUpdateDoesNotRetryAFailure(t *testing.T) {
	ut := newUpdateTest(t, fakeapi.Companion)
	ut.check(t)
	ut.u.Failed("2026.1107.2", fmt.Errorf("the new version did not start"))
	ut.updateTick(time.Now())
	if ut.upd.phase != updIdle {
		t.Fatal("tried a failed version again by itself")
	}
	tt := mg.NewTester(ut.view, 1280, 820)
	ut.page, ut.set.sec = pageSettings, secAdvanced
	tt.Frame()
	if !hasText(tt, "The last try failed: the new version did not start") {
		t.Errorf("Settings does not say why: %q", tt.Texts())
	}
}

func TestUpdateLater(t *testing.T) {
	ut := newUpdateTest(t, fakeapi.Companion)
	ut.check(t)
	tt := mg.NewTester(ut.view, 1280, 820)
	if err := tt.Click("Later"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if hasText(tt, "is available") {
		t.Errorf("the card shows after Later: %q", tt.Texts())
	}
	if st := ut.u.State(); st.Later != "2026.1107.2" || !st.LaterUntil.After(time.Now().Add(23*time.Hour)) {
		t.Errorf("state %+v", st)
	}
}

func TestUpdateWhereItCannotReplaceItself(t *testing.T) {
	ut := newUpdateTest(t, fakeapi.Companion)
	ut.u.Location = update.Location{Method: update.MethodManager, Manager: "Homebrew", Command: "brew upgrade --cask pinky"}
	ut.check(t)
	tt := mg.NewTester(ut.view, 1280, 820)
	if !hasText(tt, "Homebrew installed Pinky; update it there:") || !hasText(tt, "brew upgrade --cask pinky") {
		t.Errorf("Homebrew: %q", tt.Texts())
	}
	if hasText(tt, "Update now") {
		t.Error("Homebrew's Pinky offers to replace itself")
	}

	ut2 := newUpdateTest(t, fakeapi.Companion)
	ut2.u.Location = update.Location{Method: update.MethodDownload, Problem: "Pinky runs from its disk image. Drag it to Applications and open it from there to get updates automatically."}
	var revealed string
	ut2.reveal = func(p string) { revealed = p }
	ut2.check(t)
	ut2.updateTick(time.Now())
	if ut2.upd.phase != updIdle {
		t.Fatal("an automatic update where Pinky cannot replace itself")
	}
	tt = mg.NewTester(ut2.view, 1280, 820)
	if !hasText(tt, "Pinky runs from its disk image") {
		t.Errorf("the problem: %q", tt.Texts())
	}
	if err := tt.Click("Download Pinky 2026.1107.2"); err != nil {
		t.Fatal(err)
	}
	ut2.waitFor(t, "the download", func() bool { return revealed != "" })
	if filepath.Dir(revealed) != ut2.u.Downloads || filepath.Base(revealed) != "Pinky.zip" {
		t.Errorf("revealed %s", revealed)
	}
	if ut2.quits != 0 {
		t.Error("quit after a download for the person")
	}

	// Windows with a release whose asset is a zip, not an installer:
	// download it rather than run it.
	ut3 := newUpdateTest(t, fakeapi.Companion)
	ut3.u.Location = update.Location{Method: update.MethodInstaller, App: `C:\Users\x\AppData\Local\Programs\Pinky\Pinky.exe`}
	ut3.check(t)
	tt = mg.NewTester(ut3.view, 1280, 820)
	if hasText(tt, "Download and install") || !hasText(tt, "Download") {
		t.Errorf("a zip on Windows: %q", tt.Texts())
	}
	if m := ut3.u.MethodFor(*ut3.upd.rel); m != update.MethodDownload {
		t.Errorf("MethodFor = %v", m)
	}
}

const testChangelog = `# Changelog

## 2026.1107.2 (2026-11-07)

- Faster mirroring
- Pinky updates itself

## 2026.1107.1 (2026-11-06)

- First release
`

func TestWhatsNewAfterAnUpdate(t *testing.T) {
	ut := newUpdateTest(t, fakeapi.Companion)
	ut.u.Version, ut.opt.Version, ut.opt.Changelog = "2026.1107.2", "2026.1107.2", testChangelog
	ut.u.Change(func(s *update.State) { s.Launched("2026.1107.1") })
	ut.u.Change(func(s *update.State) { s.Launched("2026.1107.2") }) // the update ran
	tt := mg.NewTester(ut.view, 1280, 820)
	if !hasText(tt, "Pinky was updated") || !hasText(tt, "Now 2026.1107.2") {
		t.Fatalf("no What's new card: %q", tt.Texts())
	}
	if err := tt.Click("See what's new"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !hasText(tt, "What's new in Pinky 2026.1107.2") || !hasText(tt, "Pinky updates itself") {
		t.Errorf("the dialog: %q", tt.Texts())
	}
	if hasText(tt, "First release") {
		t.Errorf("the dialog shows the version that ran before: %q", tt.Texts())
	}
	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if hasText(tt, "Pinky was updated") {
		t.Errorf("the card stays after Close: %q", tt.Texts())
	}
	if st := ut.u.State(); st.NotesFrom != "" {
		t.Errorf("state %+v", st)
	}
	// Settings › Advanced shows this version's notes again.
	ut.page, ut.set.sec = pageSettings, secAdvanced
	tt.Frame()
	if err := tt.Click("Show what's new"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !hasText(tt, "Pinky updates itself") {
		t.Errorf("Settings' What's new: %q", tt.Texts())
	}
}

func TestCompanionUpdatesWhenThePhoneIsFree(t *testing.T) {
	ut := newUpdateTest(t, fakeapi.Companion)
	ut.api.SetCompanion("pixel-9", driver.CompanionOutdated)
	ut.waitFor(t, "Pixel 9", func() bool { return ut.device("pixel-9") != nil })
	ut.updateTick(time.Now()) // looks the companion up
	ut.waitFor(t, "the companion's state", func() bool { return ut.comp.state["pixel-9"] == driver.CompanionOutdated })
	tt := mg.NewTester(ut.view, 1280, 820)
	if !hasText(tt, "Update Pinky Companion") || !hasText(tt, "Pinky updates it by itself when the phone is free.") {
		t.Fatalf("the companion card: %q", tt.Texts())
	}
	ut.updateTick(time.Now())
	tt.Frame()
	if !hasText(tt, "Updating Pinky Companion") {
		t.Errorf("the card while updating: %q", tt.Texts())
	}
	ut.waitFor(t, "the companion update", func() bool { return !ut.companionUpdating("pixel-9") })
	if got := ut.api.CompanionUpdates(); len(got) != 1 || got[0] != "pixel-9" {
		t.Fatalf("updates %v", got)
	}
	if len(ut.api.Installs()) != 0 {
		t.Error("the automatic update went through the person's install")
	}
	tt.Frame()
	if hasText(tt, "Update Pinky Companion") {
		t.Errorf("the card stays: %q", tt.Texts())
	}
	if !hasText(tt, "Pinky Companion was updated") || !hasText(tt, "On Pixel 9") {
		t.Errorf("no What's new for the companion: %q", tt.Texts())
	}
	st := ut.u.State()
	if len(st.Changes) != 1 || st.Changes[0].Device != "Pixel 9" || st.Changes[0].To != "0.1.0 (130)" {
		t.Errorf("changes %+v", st.Changes)
	}
	if err := tt.Click("See what's new"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !hasText(tt, "Updated on Pixel 9 to 0.1.0 (130)") {
		t.Errorf("the dialog: %q", tt.Texts())
	}
}

func TestCompanionSignedWithThePublicKeyIsReplaced(t *testing.T) {
	ut := newUpdateTest(t, fakeapi.Companion)
	ut.api.SetCompanionSignedElsewhere("pixel-9")
	ut.waitFor(t, "Pixel 9", func() bool { return ut.device("pixel-9") != nil })
	ut.updateTick(time.Now()) // looks the companion up
	ut.waitFor(t, "the companion's state", func() bool { return ut.comp.state["pixel-9"] == driver.CompanionOutdated })
	ut.updateTick(time.Now())
	ut.waitFor(t, "the replacement", func() bool { return len(ut.api.CompanionUpdates()) == 1 && !ut.companionUpdating("pixel-9") })
	tt := mg.NewTester(ut.view, 1280, 820)
	tt.Frame()
	if !hasText(tt, "Reinstalled Pinky Companion on Pixel 9 with Pinky's private signing key") {
		t.Errorf("no notice of the replacement: %q", tt.Texts())
	}
	if st := ut.u.State(); len(st.Changes) != 1 || st.Changes[0].Kind != "companion-replaced" {
		t.Fatalf("changes %+v", st.Changes)
	}
	if err := tt.Click("See what's new"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !hasText(tt, "Reinstalled on Pixel 9 as 0.1.0 (130), signed with Pinky's private key.") {
		t.Errorf("the dialog: %q", tt.Texts())
	}
	if ut.comp.state["pixel-9"] != driver.CompanionReady {
		t.Errorf("state after the replacement: %q", ut.comp.state["pixel-9"])
	}
}

func TestCompanionWaitsWhileAnAgentWorks(t *testing.T) {
	ut := newUpdateTest(t, fakeapi.OnePhone)
	ut.waitFor(t, "the agent's task", func() bool { return ut.anyRunning() })
	ut.api.SetCompanion("pixel-9", driver.CompanionOutdated)
	ut.updateTick(time.Now())
	ut.waitFor(t, "the companion's state", func() bool { return ut.comp.state["pixel-9"] == driver.CompanionOutdated })
	for i := 0; i < 3; i++ {
		ut.updateTick(time.Now())
		ut.sync()
	}
	if ut.companionUpdating("pixel-9") || len(ut.api.CompanionUpdates()) != 0 {
		t.Error("updated the companion while an agent works on the phone")
	}
	ut.settings.AutoUpdate = false
	ut2 := newUpdateTest(t, fakeapi.Companion)
	ut2.settings.AutoUpdate = false
	ut2.api.SetCompanion("pixel-9", driver.CompanionOutdated)
	ut2.updateTick(time.Now())
	ut2.sync()
	if ut2.companionUpdating("pixel-9") {
		t.Error("updated the companion with automatic updates off")
	}
}

func TestSettingsCheckNow(t *testing.T) {
	ut := newUpdateTest(t, fakeapi.Companion)
	var saved []Settings
	ut.opt.OnSettings = func(_, s Settings) { saved = append(saved, s) }
	ut.page, ut.set.sec = pageSettings, secAdvanced
	tt := mg.NewTester(ut.view, 1280, 820)
	if !hasText(tt, "Update automatically") || !hasText(tt, "Pinky 2026.1107.1") {
		t.Fatalf("Settings › Advanced: %q", tt.Texts())
	}
	if err := tt.Click("Check now"); err != nil {
		t.Fatal(err)
	}
	ut.waitFor(t, "the check", func() bool { return !ut.upd.checking })
	tt.Frame()
	if !hasText(tt, "Pinky 2026.1107.2 is available.") {
		t.Errorf("after Check now: %q", tt.Texts())
	}
	if err := tt.Click("Update automatically"); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].AutoUpdate {
		t.Errorf("turning automatic updates off saved %+v", saved)
	}

	// The same version: up to date, said once.
	ut.u.Version = "2026.1107.2"
	ut.upd.rel = nil
	ut.checkUpdate(true)
	ut.waitFor(t, "the check", func() bool { return !ut.upd.checking })
	tt.Frame()
	if !hasText(tt, "Pinky is up to date") || !hasText(tt, "Up to date. Checked just now.") {
		t.Errorf("up to date: %q", tt.Texts())
	}
	// A development build.
	ut.u.Version, ut.opt.Version = update.DevVersion, update.DevVersion
	tt.Frame()
	if !hasText(tt, "Development builds don't update themselves.") {
		t.Errorf("a development build: %q", tt.Texts())
	}
}

func TestRelaunchArgs(t *testing.T) {
	for _, c := range []struct {
		in   []string
		bg   bool
		want []string
	}{
		{nil, false, nil},
		{nil, true, []string{"--background"}},
		{[]string{"app", "--background", "--fake"}, false, []string{"--fake"}},
		{[]string{"--theme", "dark", "-background=true"}, true, []string{"--theme", "dark", "--background"}},
	} {
		if got := relaunchArgs(c.in, c.bg); !slices.Equal(got, c.want) {
			t.Errorf("relaunchArgs(%q, %v) = %q, want %q", c.in, c.bg, got, c.want)
		}
	}
	env := relaunchEnv([]string{"HOME=/Users/x", "PINKY_HOME=/tmp/h", "PINKY_UPDATE_FEED=http://127.0.0.1:1/latest.json", "MYGO_AUTOMATION=/tmp/s", "PATH=/bin"})
	if !slices.Equal(env, []string{"PINKY_HOME=/tmp/h", "PINKY_UPDATE_FEED=http://127.0.0.1:1/latest.json", "MYGO_AUTOMATION=/tmp/s"}) {
		t.Errorf("relaunchEnv = %q", env)
	}
}

// TestUpdateScreenshots renders the update card, its notes and What's new
// in light and dark into $PINKY_SCREENSHOTS, for the notes.
func TestUpdateScreenshots(t *testing.T) {
	dir := os.Getenv("PINKY_SCREENSHOTS")
	if dir == "" {
		t.Skip("set PINKY_SCREENSHOTS to a folder to write the update screenshots")
	}
	ut := newUpdateTest(t, fakeapi.Companion)
	ut.check(t)
	shot := func(name string, dark bool) {
		t.Helper()
		tt := mg.NewTester(ut.view, 1280, 820)
		tt.SetScale(2)
		tt.SetDark(dark)
		tt.Frame()
		tt.Frame()
		if dark {
			name += "-dark"
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	for _, dark := range []bool{false, true} {
		shot("update-card", dark)
		ut.upd.notesOpen = true
		shot("update-notes", dark)
		ut.upd.notesOpen = false
	}
	ut.upd.phase, ut.upd.done, ut.upd.total = updDownloading, 18<<20, 41<<20
	shot("update-downloading", false)
	ut.upd.phase = updIdle
	// The companion's card: the update Pinky will make, then making it.
	ut.comp.state["pixel-9"], ut.comp.checked["pixel-9"] = driver.CompanionOutdated, time.Now()
	shot("companion-outdated", false)
	ut.upd.comp = map[string]*compAuto{"pixel-9": {running: true}}
	shot("companion-updating", false)
	ut.upd.comp = nil
	ut.comp.state["pixel-9"] = driver.CompanionOn
	ut.upd.rel = nil
	ut.u.Version, ut.opt.Version, ut.opt.Changelog = "2026.1107.2", "2026.1107.2", testChangelog
	ut.u.Change(func(s *update.State) { s.Launched("2026.1107.1"); s.Launched("2026.1107.2") })
	ut.u.Change(func(s *update.State) {
		s.Changes = []update.Change{{Kind: "companion", Device: "Pixel 9", From: "0.1.0 (75)", To: "0.1.0 (130)", At: time.Now()}}
	})
	for _, dark := range []bool{false, true} {
		shot("whats-new-card", dark)
		ut.upd.newsOpen = true
		shot("whats-new", dark)
		ut.upd.newsOpen = false
	}
}

func TestAutomaticUpdateBacksOffAfterAFailure(t *testing.T) {
	ut := newUpdateTest(t, fakeapi.Companion)
	ut.check(t)
	good := ut.upd.rel.Asset
	ut.upd.rel.Asset.SHA256 = hexenc.EncodeToString(make([]byte, 32)) // the download will not match
	now := time.Now()
	ut.updateTick(now)
	ut.waitFor(t, "the failed download", func() bool { return ut.upd.phase == updIdle && ut.upd.failures == 1 })
	if ut.quits != 0 || ut.u.State().Failed != "" {
		t.Fatalf("a damaged download: quits %d, state %+v", ut.quits, ut.u.State())
	}
	if d := ut.upd.retryAt.Sub(now); d < 59*time.Minute || d > 61*time.Minute {
		t.Errorf("retry in %v, want an hour", d)
	}
	ut.updateTick(now.Add(30 * time.Minute))
	if ut.upd.phase != updIdle {
		t.Fatal("tried again before the hour")
	}
	ut.upd.rel.Asset = good
	ut.updateTick(now.Add(61 * time.Minute))
	ut.waitFor(t, "the second try", func() bool { return ut.quits > 0 })
	if ut.upd.failures != 0 {
		t.Errorf("failures %d after a good install", ut.upd.failures)
	}
}
