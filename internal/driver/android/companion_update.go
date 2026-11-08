package android

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/driver/android/companion"
)

// UpdateCompanion implements driver.CompanionUpdater: it installs the
// bundled companion APK over an older installed build, as `pm install -r`
// does, without asking anyone. A build signed with another key, such as
// one signed with the public key every build had until 2026-11-08, cannot
// be updated in place: it is turned off, uninstalled (Pinky's own package
// only) and replaced. Either way the service is off afterwards, as after
// any task: Pinky turns it on while an agent works on the phone, and the
// person may be using the phone now. It refuses with driver.ErrNeedsPerson
// when the phone would show a dialog (Google Play Protect checks adb
// installs), and does nothing while the driver has the service on.
func (d *Device) UpdateCompanion(ctx context.Context) (driver.CompanionUpdate, error) {
	var res driver.CompanionUpdate
	c := d.newCompanion()
	defer c.Close()
	before, installed, err := c.InstalledVersion(ctx)
	if err != nil {
		return res, wrapErr(err)
	}
	if !installed {
		return res, fmt.Errorf("the companion app is not installed: %w", driver.ErrUnsupported)
	}
	apk, err := FindCompanionAPK()
	if err != nil {
		return res, fmt.Errorf("%w: %w", err, driver.ErrUnsupported)
	}
	_, bundled, err := apkVersion(apk)
	if err != nil {
		return res, err
	}
	res.From, res.To = versionLine(before), versionLine(bundled)
	replace := before.SignedElsewhere(bundled)
	if !replace && (before.Code <= 0 || before.Code >= bundled.Code) {
		res.To = res.From
		res.ServiceOn, _ = c.Enabled(ctx)
		return res, nil
	}
	if d.companionInUse() {
		return res, errCompanionBusy
	}
	if asks, why := installAsks(ctx, d.companionRunner()); asks {
		return res, fmt.Errorf("%s: %w", why, driver.ErrNeedsPerson)
	}
	// The app's process goes away during the install: close the driver's
	// link to it first, so its reads fall back cleanly, and look at the
	// app afresh afterwards.
	d.companionOff("updating the app")
	if !replace {
		err = c.Install(ctx, apk)
		replace = err != nil && strings.Contains(err.Error(), "INSTALL_FAILED_UPDATE_INCOMPATIBLE")
		if err != nil && !replace {
			return res, wrapErr(err)
		}
	}
	if replace {
		d.cfg.logf("android: %s: the installed companion is signed with %s; replacing it", d.info.ID, signerLine(before.Signer))
		if err := d.replaceCompanion(ctx, c, apk); err != nil {
			return res, err
		}
		res.Replaced = true
		res.OldPublicKey = before.Signer == companion.OldPublicSigner
	}
	res.Updated = true
	k := &d.comp
	k.mu.Lock()
	k.checked = false
	k.retryAt = time.Time{}
	k.mu.Unlock()
	// Android keeps a service listed across an update of the same package
	// and binds the new build: one that an older Pinky kept on for good
	// goes off, unless another client uses it.
	if listed, err := c.Enabled(ctx); err == nil && listed {
		if answers(ctx, c, 10*time.Second) && companionPeers(ctx, c) {
			res.ServiceOn = true
		} else if err := c.ForceDisable(ctx); err != nil {
			d.cfg.logf("android: %s: turning the updated companion's service off: %v", d.info.ID, err)
			res.ServiceOn = true
		}
	}
	d.cfg.logf("android: %s: updated the companion %s -> %s (replaced: %v)", d.info.ID, res.From, res.To, res.Replaced)
	return res, nil
}

// answers waits up to max for the companion app's service to answer.
func answers(ctx context.Context, c *companion.Client, max time.Duration) bool {
	deadline := time.Now().Add(max)
	for {
		pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := c.Ping(pctx)
		cancel()
		if err == nil {
			return true
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return false
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// versionLine is a companion version as people read it: "0.1.0 (75)".
func versionLine(v companion.Version) string {
	switch {
	case v.Name != "" && v.Code > 0:
		return fmt.Sprintf("%s (%d)", v.Name, v.Code)
	case v.Code > 0:
		return fmt.Sprintf("build %d", v.Code)
	}
	return v.Name
}

// installAsks reports whether an install over adb may show the person a
// dialog on the phone: Google Play Protect checks such installs where
// Google Play services are installed, unless verifier_verify_adb_installs
// or package_verifier_enable is 0 (unset means on). Reading the settings
// changes nothing.
func installAsks(ctx context.Context, run companion.Runner) (bool, string) {
	out, err := run.Shell(ctx, "if pm path com.google.android.gms >/dev/null 2>&1; then echo gms=1; else echo gms=0; fi; "+
		"echo adb=$(settings get global verifier_verify_adb_installs); echo verifier=$(settings get global package_verifier_enable)")
	if err != nil {
		return true, "could not read the phone's install checks"
	}
	v := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, val, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			v[k] = strings.TrimSpace(val)
		}
	}
	if v["gms"] != "1" || v["adb"] == "0" || v["verifier"] == "0" {
		return false, ""
	}
	return true, "Google Play Protect checks installs over USB on this phone"
}

// errCompanionBusy: the driver has the service on for a task's reads, which
// end within seconds of the task; the update waits for that.
var errCompanionBusy = errors.New("android: the driver is using the companion app; try again in a minute")

var _ driver.CompanionUpdater = (*Device)(nil)
