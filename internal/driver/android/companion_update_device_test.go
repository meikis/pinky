//go:build device

package android

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/driver/android/companion"
)

// TestDeviceCompanionUpdate updates the companion app the way automatic
// updates do, on an emulator, then runs a task's worth of the service:
//
//  1. an older build with its service off is updated and stays off;
//  2. an older build with its service on for good, as Pinky before
//     2026-11-08 left it, is updated and its service goes off: Pinky turns
//     it on only while an agent works;
//  3. an agent starts working: the service comes on by itself, the first
//     read finds it, and it goes off a few seconds after the agent stops.
//
// When the two builds are signed with different keys (an old build signed
// with the public key, a release build), the update replaces the app, and
// the person's other accessibility services are kept. It needs the older
// build (PINKY_COMPANION_OLD) and the one Pinky bundles
// (PINKY_COMPANION_APK); PINKY_COMPANION_RESTORE puts back the build the
// phone had (else the bundled one stays). It installs and removes Pinky's
// own app and turns its service on and off, so it runs on emulators only,
// and it leaves the setting as it found it:
//
//	scripts/devlock.sh emulator-5560 env PINKY_DEVICE=emulator-5560 \
//	  PINKY_COMPANION_OLD=old.apk PINKY_COMPANION_APK=new.apk \
//	  go test -tags device -run TestDeviceCompanionUpdate -v ./internal/driver/android/
func TestDeviceCompanionUpdate(t *testing.T) {
	serial := os.Getenv("PINKY_DEVICE")
	if !strings.HasPrefix(serial, "emulator-") {
		t.Skip("emulators only: it installs apps and turns Pinky's service on and off")
	}
	oldAPK, newAPK, restoreAPK := os.Getenv("PINKY_COMPANION_OLD"), os.Getenv("PINKY_COMPANION_APK"), os.Getenv("PINKY_COMPANION_RESTORE")
	if oldAPK == "" || newAPK == "" {
		t.Skip("PINKY_COMPANION_OLD and PINKY_COMPANION_APK not set")
	}
	oldApp, oldV, err := companion.APKVersion(oldAPK)
	if err != nil {
		t.Fatal(err)
	}
	app, newV, err := companion.APKVersion(newAPK)
	if err != nil {
		t.Fatal(err)
	}
	if oldApp != app {
		t.Fatalf("the old build is %s, the new one %s", oldApp.Package, app.Package)
	}
	if oldV.Code >= newV.Code && !oldV.SignedElsewhere(newV) {
		t.Fatalf("the old build (%d) is not older than the new one (%d)", oldV.Code, newV.Code)
	}
	replaces := oldV.SignedElsewhere(newV)
	t.Logf("old %s (%d, signer %s), new %s (%d, signer %s), replaced: %v", oldV.Name, oldV.Code, oldV.Signer, newV.Name, newV.Code, newV.Signer, replaces)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	p := NewProvider(Config{Logf: t.Logf})
	d, err := p.OpenDevice(ctx, serial)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	run := adbRunner{c: d.client, serial: serial}
	c := companion.NewFor(run, app)
	defer c.Close()

	shell := func(cmd string) string {
		t.Helper()
		out, err := run.Shell(ctx, cmd)
		if err != nil {
			t.Fatalf("%s: %v", cmd, err)
		}
		return strings.TrimSpace(out)
	}
	setting := func() string { return shell("settings get secure enabled_accessibility_services") }
	listed := func() bool { return strings.Contains(setting(), app.Package+"/") }
	installed := func() companion.Version {
		v, ok, err := c.InstalledVersion(ctx)
		if err != nil || !ok {
			t.Fatalf("installed version: %v, %v", ok, err)
		}
		return v
	}
	// put installs a build over whatever is there: a downgrade (-d, for
	// debuggable builds) or, when the keys differ or Android refuses, after
	// removing the installed one with its service off.
	put := func(apk string) {
		t.Helper()
		_, want, err := companion.APKVersion(apk)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(apk)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		var b [6]byte
		rand.Read(b[:])
		remote := "/data/local/tmp/pinky-companion-put-" + hex.EncodeToString(b[:]) + ".apk"
		if err := d.client.Push(ctx, serial, f, remote, 0o644, time.Now()); err != nil {
			t.Fatal(err)
		}
		defer shell("rm -f " + remote)
		out := shell("pm install -r -d -t " + remote + " 2>&1 || true")
		if !strings.Contains(out, "Success") {
			if err := c.ForceDisable(ctx); err != nil {
				t.Fatal(err)
			}
			shell("pm uninstall " + app.Package + " 2>&1 || true")
			if out := shell("pm install -t " + remote + " 2>&1 || true"); !strings.Contains(out, "Success") {
				t.Fatalf("install %s: %s", apk, out)
			}
		}
		if got := installed(); got.Code != want.Code || got.Signer != want.Signer {
			t.Fatalf("installed %+v, want %+v", got, want)
		}
	}
	orig := setting()
	origIdle := int64(-1)
	if listed() {
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		if info, err := c.Ping(pctx); err == nil {
			origIdle = info.IdleDisableMS
		}
		cancel()
	}
	t.Logf("before: build %+v, enabled_accessibility_services=%q, idle_disable_ms=%d", installed(), orig, origIdle)
	defer func() {
		rctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		ctx = rctx
		if restoreAPK != "" {
			put(restoreAPK)
		}
		cmd := "settings put secure enabled_accessibility_services '" + strings.ReplaceAll(orig, "'", `'\''`) + "'"
		if orig == "null" || orig == "" {
			cmd = "settings delete secure enabled_accessibility_services"
		}
		if _, err := run.Shell(rctx, cmd); err != nil {
			t.Errorf("restore the setting: %v", err)
		}
		// The app's own settings went with an uninstall.
		if origIdle >= 0 && answers(rctx, c, 15*time.Second) {
			if err := c.SetIdleDisable(rctx, time.Duration(origIdle)*time.Millisecond); err != nil {
				t.Errorf("restore idle_disable_ms: %v", err)
			}
		}
		t.Logf("after: build %+v, enabled_accessibility_services=%q", installed(), setting())
	}()
	t.Setenv("PINKY_COMPANION_APK", newAPK)
	// The gateway tells the driver whether an agent works; nobody does yet.
	d.AgentWorking(false)
	time.Sleep(time.Second)

	// 1. Off stays off.
	put(oldAPK)
	if listed() {
		if err := c.ForceDisable(ctx); err != nil {
			t.Fatal(err)
		}
	}
	before := setting()
	if st, err := d.CompanionState(ctx); err != nil || st != driver.CompanionOutdated {
		t.Fatalf("CompanionState = %v, %v; want outdated", st, err)
	}
	res, err := d.UpdateCompanion(ctx)
	if err != nil {
		t.Fatalf("UpdateCompanion (service off): %v", err)
	}
	if !res.Updated || res.ServiceOn || res.Replaced != replaces || res.To != versionLine(newV) || res.From != versionLine(oldV) {
		t.Errorf("UpdateCompanion (service off) = %+v", res)
	}
	if got := installed(); got.Code != newV.Code || got.Signer != newV.Signer {
		t.Errorf("installed %+v after the update", got)
	}
	if after := setting(); after != before {
		t.Errorf("the update changed the setting: %q -> %q", before, after)
	}
	if res, err := d.UpdateCompanion(ctx); err != nil || res.Updated {
		t.Errorf("a second UpdateCompanion = %+v, %v; want nothing to do", res, err)
	}

	// 2. On for good, as Pinky until 2026-11-08 left it: off after the
	// update, the person's other services kept.
	put(oldAPK)
	others := strings.Trim(strings.ReplaceAll(strings.ReplaceAll(setting(), oldApp.Component(), ""), "::", ":"), ":")
	if err := c.TurnOn(ctx); err != nil {
		t.Fatalf("turn the service on: %v", err)
	}
	if !answers(ctx, c, 15*time.Second) {
		t.Fatal("the old build's service does not answer")
	}
	res, err = d.UpdateCompanion(ctx)
	if err != nil {
		t.Fatalf("UpdateCompanion (service on): %v", err)
	}
	if !res.Updated || res.ServiceOn || res.Replaced != replaces {
		t.Errorf("UpdateCompanion (service on) = %+v", res)
	}
	if listed() {
		t.Errorf("the service is on after the update: %q", setting())
	}
	if others != "" && others != "null" && !strings.Contains(setting(), others) {
		t.Errorf("the person's other services %q are gone: %q", others, setting())
	}
	if st, err := d.CompanionState(ctx); err != nil || st != driver.CompanionReady {
		t.Errorf("CompanionState after the update = %v, %v; want ready", st, err)
	}

	// 3. A task: the service comes on as it starts, the first read finds
	// it, and it goes off a few seconds after the task.
	start := time.Now()
	d.AgentWorking(true)
	s, err := d.Observe(ctx, driver.ObserveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first := time.Since(start)
	if !d.companionInUse() {
		t.Fatal("the first read of the task did without the companion")
	}
	t.Logf("first read of the task %s after it started: %d elements in %s", first.Round(time.Millisecond), len(s.Elements), s.App.ID)
	start = time.Now()
	if _, err := d.Observe(ctx, driver.ObserveOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Logf("next read %s", time.Since(start).Round(time.Millisecond))
	if len(s.Elements) == 0 && !s.Locked {
		t.Errorf("no elements on an unlocked screen:\n%s", s.Render())
	}
	d.AgentWorking(false)
	stop := time.Now()
	for d.companionInUse() || listed() {
		if time.Since(stop) > 30*time.Second {
			t.Fatal("the service is still on 30 s after the task")
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("off %s after the task", time.Since(stop).Round(100*time.Millisecond))
	if st, err := d.CompanionState(ctx); err != nil || st != driver.CompanionReady {
		t.Errorf("CompanionState after the task = %v, %v; want ready", st, err)
	}
}
