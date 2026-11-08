package android

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/driver/android/companion"
	"github.com/DIYgod/pinky/internal/driver/android/companion/companiontest"
)

// quietPhone plays a phone with Pinky Companion for the driver: its
// accessibility setting, its package manager, and the app's socket, which
// answers while the service is listed and drops every connection when it
// stops being listed, as the service does when it is turned off.
type quietPhone struct {
	t  *testing.T
	ln net.Listener

	mu         sync.Mutex
	setting    string // enabled_accessibility_services; "" with unset
	unset      bool
	app        companion.App
	installed  bool
	code       int
	signer     string
	installErr string // what the next pm install says instead of Success
	installs   int
	uninstalls int
	writes     int // settings put and delete
	conns      map[net.Conn]bool
	others     int // connections of other clients, counted in ping
}

func init() { companionRest = 100 * time.Millisecond }

func newQuietPhone(t *testing.T) *quietPhone {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &quietPhone{t: t, ln: ln, unset: true, app: companion.Release, installed: true, code: 400,
		signer: "5ca1ab1e", conns: map[net.Conn]bool{}}
	go p.serve()
	t.Cleanup(func() {
		ln.Close()
		p.dropAll()
	})
	return p
}

func (p *quietPhone) listedLocked() bool {
	for _, s := range strings.Split(p.setting, ":") {
		if s == p.app.Component() && p.installed {
			return true
		}
	}
	return false
}

func (p *quietPhone) listed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.listedLocked()
}

func (p *quietPhone) dropAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for c := range p.conns {
		c.Close()
	}
	p.conns = map[net.Conn]bool{}
}

func (p *quietPhone) serve() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		p.mu.Lock()
		on := p.listedLocked()
		if on {
			p.conns[c] = true
		}
		p.mu.Unlock()
		if !on {
			c.Close()
			continue
		}
		go p.handle(c)
	}
}

func (p *quietPhone) handle(c net.Conn) {
	defer func() {
		p.mu.Lock()
		delete(p.conns, c)
		p.mu.Unlock()
		c.Close()
	}()
	rd := bufio.NewReader(c)
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			return
		}
		var req map[string]any
		if err := json.Unmarshal(line, &req); err != nil {
			return
		}
		var result any
		switch req["cmd"] {
		case "ping":
			p.mu.Lock()
			n := len(p.conns) + p.others
			p.mu.Unlock()
			result = map[string]any{"protocol": companion.ProtocolVersion, "version": "2026.1108.9", "connections": n,
				"idle_disable_ms": 60000, "time": time.Now().UnixMilli(), "displays": []int{0}}
		case "events":
			result = map[string]any{"kinds": req["kinds"]}
		case "tree":
			result = map[string]any{"display": map[string]any{"id": 0, "width": 1080, "height": 2400}, "windows": []any{}, "time": time.Now().UnixMilli()}
		default:
			result = map[string]any{}
		}
		b, _ := json.Marshal(map[string]any{"id": req["id"], "ok": true, "result": result})
		if _, err := c.Write(append(b, '\n')); err != nil {
			return
		}
	}
}

func (p *quietPhone) Shell(_ context.Context, cmd string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	const key = "enabled_accessibility_services"
	switch {
	case cmd == "dumpsys package "+p.app.Package:
		if !p.installed {
			return "", nil
		}
		return fmt.Sprintf("Packages:\n  Package [%s] (5e1a2f0):\n    versionCode=%d minSdk=29 targetSdk=36\n    versionName=2026.1108.%d\n"+
			"    signatures=PackageSignatures{97e501d version:2, signatures:[%s], past signatures:[]}\n", p.app.Package, p.code, p.code, p.signer), nil
	case strings.HasPrefix(cmd, "dumpsys package "):
		return "", nil
	case cmd == "settings get secure "+key:
		if p.unset {
			return "null\n", nil
		}
		return p.setting + "\n", nil
	case strings.HasPrefix(cmd, "settings put secure "+key+" "):
		p.setting, p.unset = strings.Trim(strings.TrimPrefix(cmd, "settings put secure "+key+" "), "'"), false
		p.writes++
	case cmd == "settings delete secure "+key:
		p.setting, p.unset = "", true
		p.writes++
	case cmd == "pm uninstall "+p.app.Package:
		p.installed = false
		p.uninstalls++
		return "Success\n", nil
	case strings.HasPrefix(cmd, "if pm path com.google.android.gms"):
		return "gms=0\nadb=null\nverifier=null\n", nil
	default:
		return "", fmt.Errorf("quietPhone: unexpected shell %q", cmd)
	}
	if !p.listedLocked() {
		go p.dropAll() // the service unbinds
	}
	return "", nil
}

func (p *quietPhone) Forward(context.Context, string) (string, error) {
	return p.ln.Addr().String(), nil
}

func (p *quietPhone) RemoveForward(context.Context, string) error { return nil }

func (p *quietPhone) Install(_ context.Context, apk string) error {
	app, v, err := companion.APKVersion(apk)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.installErr != "" {
		msg := p.installErr
		p.installErr = ""
		return errors.New("android: install: " + msg)
	}
	if p.installed && p.signer != v.Signer {
		return errors.New("android: install: Failure [INSTALL_FAILED_UPDATE_INCOMPATIBLE: signatures do not match]")
	}
	p.app, p.installed, p.code, p.signer = app, true, v.Code, v.Signer
	p.installs++
	return nil
}

// get reads one of the phone's fields under its lock.
func get[T any](p *quietPhone, f func() T) T {
	p.mu.Lock()
	defer p.mu.Unlock()
	return f()
}

// quietDevice is a device on the fake phone, carrying apk as its bundled
// companion, and what it logged so far.
func quietDevice(t *testing.T, p *quietPhone, apk string) (*Device, func() []string) {
	t.Helper()
	if apk != "" {
		t.Setenv("PINKY_COMPANION_APK", apk)
	} else {
		t.Setenv("PINKY_COMPANION_APK", "/nonexistent/pinky-companion.apk")
	}
	var logs []string
	var mu sync.Mutex
	cfg := Config{Logf: func(f string, a ...any) {
		mu.Lock()
		logs = append(logs, fmt.Sprintf(f, a...))
		mu.Unlock()
		t.Logf(f, a...)
	}}
	cfg.defaults()
	d := newDevice(cfg, driver.Info{ID: "phone"}, deviceProps{})
	d.compRun = p
	t.Cleanup(d.closeCompanion)
	return d, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(logs)
	}
}

// waitFor polls cond for up to 5 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCompanionOnlyWhileAgentsWork(t *testing.T) {
	p := newQuietPhone(t)
	d, _ := quietDevice(t, p, "")
	ctx := context.Background()

	// The gateway opened the phone with no task: nothing comes on, and
	// reads do without the companion.
	d.AgentWorking(false)
	if c := d.companionClient(ctx); c != nil {
		t.Fatal("the companion came on with no agent working")
	}
	time.Sleep(50 * time.Millisecond)
	if w := get(p, func() int { return p.writes }); p.listed() || w != 0 {
		t.Fatalf("the setting changed with no agent working: %d writes", w)
	}

	// A task starts: the service comes on by itself, before any read.
	d.AgentWorking(true)
	waitFor(t, "the service is on for the task", func() bool { return d.companionInUse() && p.listed() })
	if c := d.companionClient(ctx); c == nil {
		t.Fatal("no companion while an agent works")
	}

	// The task ends: off a few seconds after the last call, and the
	// setting is as it was (unset).
	d.AgentWorking(false)
	waitFor(t, "the service is off after the task", func() bool { return !d.companionInUse() && !p.listed() })
	if !get(p, func() bool { return p.unset }) {
		t.Errorf("the setting is %q after the task, was unset", get(p, func() string { return p.setting }))
	}
	if st, err := d.CompanionState(ctx); err != nil || st != driver.CompanionReady {
		t.Errorf("CompanionState after the task = %q, %v; want ready", st, err)
	}
}

func TestCompanionForUnlockWithoutATask(t *testing.T) {
	p := newQuietPhone(t)
	d, _ := quietDevice(t, p, "")
	d.AgentWorking(false)
	if c := d.companionClientFor(context.Background(), forUnlock); c == nil {
		t.Fatal("no companion for unlocking")
	}
	if !p.listed() {
		t.Fatal("the service is not on for unlocking")
	}
	waitFor(t, "the service is off after unlocking", func() bool { return !p.listed() })
}

func TestCompanionLeftOnGoesOff(t *testing.T) {
	// Pinky until 2026-11-08 kept the service on for good after Install.
	p := newQuietPhone(t)
	mika := "com.wangzan101.mika/.a11y.MikaAccessibilityService"
	p.setting, p.unset = mika+":"+companion.Release.Component(), false
	d, logs := quietDevice(t, p, "")
	d.AgentWorking(false)
	waitFor(t, "the service left on is off", func() bool { return !p.listed() })
	if got := get(p, func() string { return p.setting }); got != mika {
		t.Errorf("setting = %q, want the person's other service %q", got, mika)
	}
	waitFor(t, "it says so", func() bool {
		return slices.ContainsFunc(logs(), func(l string) bool { return strings.Contains(l, "turned off the companion's service") })
	})

	// One that another client uses stays on.
	p2 := newQuietPhone(t)
	p2.setting, p2.unset, p2.others = companion.Release.Component(), false, 1
	d2, _ := quietDevice(t, p2, "")
	d2.AgentWorking(false)
	time.Sleep(300 * time.Millisecond)
	if !p2.listed() {
		t.Error("turned off a service another client uses")
	}
}

func TestCompanionStateRefused(t *testing.T) {
	p := newQuietPhone(t)
	d, _ := quietDevice(t, p, "")
	ctx := context.Background()
	if st, _ := d.CompanionState(ctx); st != driver.CompanionReady {
		t.Fatalf("state = %q, want ready", st)
	}
	// The phone refuses settings changes from the computer (Xiaomi without
	// USB debugging (Security settings)): the next task finds it off.
	d.compRun = refusingRunner{p}
	d.comp.client = nil
	d.AgentWorking(true)
	if c := d.companionClient(ctx); c != nil {
		t.Fatal("used a companion whose service could not come on")
	}
	if st, _ := d.CompanionState(ctx); st != driver.CompanionOff {
		t.Errorf("state after a refusal = %q, want off", st)
	}
	// Turn on (SetUpCompanion) says why.
	_, err := d.SetUpCompanion(ctx, false)
	if err == nil || !strings.Contains(err.Error(), "WRITE_SECURE_SETTINGS") {
		t.Errorf("SetUpCompanion = %v, want the phone's refusal", err)
	}
}

// refusingRunner refuses settings changes, as some phones do over adb.
type refusingRunner struct{ *quietPhone }

func (r refusingRunner) Shell(ctx context.Context, cmd string) (string, error) {
	if strings.HasPrefix(cmd, "settings put") || strings.HasPrefix(cmd, "settings delete") {
		return "", errors.New("adb shell settings: exit status 255: java.lang.SecurityException: Permission denial: writing to settings requires:android.permission.WRITE_SECURE_SETTINGS")
	}
	return r.quietPhone.Shell(ctx, cmd)
}

func TestUpdateCompanionReplacesTheOldPublicKey(t *testing.T) {
	cert, err := os.ReadFile("companion/testdata/old-public-signer.der")
	if err != nil {
		t.Fatal(err)
	}
	if companion.SignerName(cert) != companion.OldPublicSigner {
		t.Fatal("the test certificate is not the old public one")
	}
	release := []byte("Pinky's release certificate")
	newSigner := companion.SignerName(release)
	apk := companiontest.APK(t, companion.Release.Package, "2026.1108.9", 480, release)

	// Release 2026.1108.3 on the Mi 10 Pro: signed with the public key, its
	// service on for good beside the person's own.
	p := newQuietPhone(t)
	mika := "com.wangzan101.mika/.a11y.MikaAccessibilityService"
	p.setting, p.unset = mika+":"+companion.Release.Component(), false
	p.code, p.signer = 384, companion.OldPublicSigner
	d, _ := quietDevice(t, p, apk)
	ctx := context.Background()

	if st, err := d.CompanionState(ctx); err != nil || st != driver.CompanionOutdated {
		t.Fatalf("state = %q, %v; want outdated", st, err)
	}
	res, err := d.UpdateCompanion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Updated || !res.Replaced || !res.OldPublicKey || res.ServiceOn {
		t.Errorf("result = %+v", res)
	}
	p.mu.Lock()
	signer, uninstalls, installs, setting := p.signer, p.uninstalls, p.installs, p.setting
	p.mu.Unlock()
	if signer != newSigner || uninstalls != 1 || installs != 1 {
		t.Errorf("signer %s after %d uninstalls and %d installs", signer, uninstalls, installs)
	}
	// The person's other service is kept, and Pinky's is off until a task.
	if setting != mika {
		t.Errorf("setting = %q, want %q", setting, mika)
	}
	if st, _ := d.CompanionState(ctx); st != driver.CompanionReady {
		t.Errorf("state after the update = %q, want ready", st)
	}
	// The next task finds the new build.
	d.AgentWorking(true)
	waitFor(t, "the new build is on for a task", func() bool { return d.companionInUse() })

	// Nothing to do the second time.
	d.AgentWorking(false)
	waitFor(t, "off after the task", func() bool { return !d.companionInUse() })
	if res, err := d.UpdateCompanion(ctx); err != nil || res.Updated {
		t.Errorf("second update = %+v, %v", res, err)
	}
}

func TestUpdateCompanionReplacesAnUnknownKey(t *testing.T) {
	// dumpsys printed no signer: the install's refusal tells.
	apk := companiontest.APK(t, companion.Release.Package, "2026.1108.9", 480, []byte("release"))
	p := newQuietPhone(t)
	p.code, p.signer = 300, ""
	d, _ := quietDevice(t, p, apk)
	res, err := d.UpdateCompanion(context.Background())
	if err != nil || !res.Replaced || res.OldPublicKey {
		t.Errorf("result = %+v, %v", res, err)
	}
}

func TestSetUpCompanionReplacesAndTurnsOff(t *testing.T) {
	apk := companiontest.APK(t, companion.Release.Package, "2026.1108.9", 480, []byte("release"))
	p := newQuietPhone(t)
	p.code, p.signer = 384, companion.OldPublicSigner
	d, _ := quietDevice(t, p, apk)
	d.AgentWorking(false)
	res, err := d.SetUpCompanion(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Installed || !res.Replaced {
		t.Errorf("result = %+v", res)
	}
	// It came on to check, and goes off again with no agent working.
	waitFor(t, "the service is off after the check", func() bool { return !p.listed() && !d.companionInUse() })
}

func TestDevelopmentBuildIsItsOwnApp(t *testing.T) {
	apk := companiontest.APK(t, companion.Dev.Package, "0.0.0-dev", 470, []byte("a developer's key"))
	if app := func() companion.App { t.Setenv("PINKY_COMPANION_APK", apk); return companionApp() }(); app != companion.Dev {
		t.Errorf("companionApp = %+v, want the development build", app)
	}
	// A phone with only the release build: the development Pinky offers to
	// install its own, and leaves the release one alone.
	p := newQuietPhone(t)
	d, _ := quietDevice(t, p, apk)
	if st, err := d.CompanionState(context.Background()); err != nil || st != driver.CompanionMissing {
		t.Errorf("state = %q, %v; want missing", st, err)
	}
}
