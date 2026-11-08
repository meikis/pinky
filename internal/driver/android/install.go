package android

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/driver/android/adb"
	"github.com/DIYgod/pinky/internal/driver/android/companion"
)

// adbRunner lets the companion client use the driver's adb client instead
// of the adb command-line tool.
type adbRunner struct {
	c      *adb.Client
	serial string
}

// Shell implements companion.Runner; a non-zero exit status is an error.
func (r adbRunner) Shell(ctx context.Context, cmd string) (string, error) {
	res, err := r.c.Run(ctx, r.serial, cmd)
	if err != nil {
		return "", wrapErr(err)
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(string(res.Stderr))
		if msg == "" {
			msg = strings.TrimSpace(string(res.Stdout))
		}
		return string(res.Stdout), fmt.Errorf("adb shell %s: exit status %d: %s", firstWord(cmd), res.ExitCode, msg)
	}
	return string(res.Stdout), nil
}

// Forward implements companion.Runner.
func (r adbRunner) Forward(ctx context.Context, remote string) (string, error) {
	local, err := r.c.Forward(ctx, r.serial, "tcp:0", remote, false)
	if err != nil {
		return "", wrapErr(err)
	}
	return net.JoinHostPort("127.0.0.1", strings.TrimPrefix(local, "tcp:")), nil
}

// RemoveForward implements companion.Runner.
func (r adbRunner) RemoveForward(ctx context.Context, local string) error {
	_, port, err := net.SplitHostPort(local)
	if err != nil {
		return err
	}
	return wrapErr(r.c.RemoveForward(ctx, r.serial, "tcp:"+port))
}

// Install implements companion.Runner like `adb install -r`: it pushes the
// APK to a temporary file and installs it with the package manager.
func (r adbRunner) Install(ctx context.Context, apk string) error {
	f, err := os.Open(apk)
	if err != nil {
		return err
	}
	defer f.Close()
	var b [6]byte
	_, _ = rand.Read(b[:])
	remote := "/data/local/tmp/pinky-companion-" + hex.EncodeToString(b[:]) + ".apk"
	if err := r.c.Push(ctx, r.serial, f, remote, 0o644, time.Now()); err != nil {
		return wrapErr(err)
	}
	res, err := r.c.Run(ctx, r.serial, "pm install -r -t "+remote+"; status=$?; rm -f "+remote+"; exit $status")
	if err != nil {
		return wrapErr(err)
	}
	out := strings.TrimSpace(string(res.Stdout) + " " + string(res.Stderr))
	if res.ExitCode != 0 || !strings.Contains(out, "Success") {
		return fmt.Errorf("android: install %s: %s", filepath.Base(apk), out)
	}
	return nil
}

func firstWord(s string) string {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i]
	}
	return s
}

// errNoCompanionAPK means FindCompanionAPK looked everywhere and found none.
var errNoCompanionAPK = errors.New("android: no companion APK found")

// companionAPKCandidates lists where FindCompanionAPK looks, in order.
func companionAPKCandidates() []string {
	var c []string
	if p := os.Getenv("PINKY_COMPANION_APK"); p != "" {
		c = append(c, p)
	}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		dir := filepath.Dir(exe)
		c = append(c,
			filepath.Join(dir, "pinky-companion.apk"),
			// Pinky.app/Contents/MacOS/pinky -> Pinky.app/Contents/Resources
			filepath.Join(dir, "..", "Resources", "pinky-companion.apk"),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		c = append(c, filepath.Join(wd, "android", "companion", "app", "build", "outputs", "apk", "debug", "app-debug.apk"))
	}
	return c
}

// FindCompanionAPK returns the companion APK to install: $PINKY_COMPANION_APK,
// then pinky-companion.apk next to the executable, then
// ../Resources/pinky-companion.apk relative to it (a macOS app bundle),
// then the debug build under the working directory
// (android/companion/app/build/outputs/apk/debug/app-debug.apk).
func FindCompanionAPK() (string, error) {
	cands := companionAPKCandidates()
	for _, p := range cands {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return filepath.Clean(p), nil
		}
	}
	return "", fmt.Errorf("%w; looked in %s", errNoCompanionAPK, strings.Join(cands, ", "))
}

// CompanionInstall says what SetUpCompanion did.
type CompanionInstall struct {
	APK       string            // the bundled APK compared or installed
	Bundled   companion.Version // its version; zero when it has no version file
	Before    companion.Version // the installed version before; zero when it was missing
	Installed bool              // the APK was installed
	Replaced  bool              // an installed build signed with another key was uninstalled first
}

// CompanionState implements driver.CompanionInstaller: missing (or no_apk
// when Pinky has no APK to install either); outdated when the installed
// versionCode is lower than the bundled APK's, or it is signed with another
// key; on when its accessibility service is on now (an agent works); off
// when it is off and did not come on the last time the driver tried; ready
// otherwise: Pinky turns it on while an agent works.
func (d *Device) CompanionState(ctx context.Context) (driver.CompanionState, error) {
	c := d.newCompanion()
	defer c.Close()
	inst, installed, err := c.InstalledVersion(ctx)
	if err != nil {
		return "", wrapErr(err)
	}
	apk, apkErr := FindCompanionAPK()
	if !installed {
		if apkErr != nil {
			return driver.CompanionNoAPK, nil
		}
		return driver.CompanionMissing, nil
	}
	if apkErr == nil {
		if _, bundled, err := apkVersion(apk); err == nil && (inst.Code > 0 && inst.Code < bundled.Code || inst.SignedElsewhere(bundled)) {
			return driver.CompanionOutdated, nil
		}
	}
	if d.companionInUse() {
		return driver.CompanionOn, nil
	}
	listed, err := c.Enabled(ctx)
	if err != nil {
		return "", wrapErr(err)
	}
	if listed {
		pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if _, err := c.Ping(pctx); err == nil {
			return driver.CompanionOn, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
	if d.companionFailure() != nil {
		return driver.CompanionOff, nil
	}
	return driver.CompanionReady, nil
}

// InstallCompanion implements driver.CompanionInstaller with SetUpCompanion.
func (d *Device) InstallCompanion(ctx context.Context) error {
	_, err := d.SetUpCompanion(ctx, false)
	return err
}

// SetUpCompanion installs the bundled companion APK when the phone has none
// or an older build (or, with force, any build that differs from it), and
// checks that its accessibility service comes on, keeping the person's
// other services. It is for setup only, when the person asks: the driver
// never installs anything by itself. A build signed with another key
// cannot be updated in place; it is turned off, uninstalled (Pinky's own
// package only) and replaced. On phones whose Google Play Protect checks
// adb installs, the phone asks the person to allow the app, and the
// install waits for the answer, up to ctx. The service goes off again a
// few seconds later, unless an agent works on the phone: Pinky turns it on
// only while one does.
func (d *Device) SetUpCompanion(ctx context.Context, force bool) (CompanionInstall, error) {
	var res CompanionInstall
	c := d.newCompanion()
	defer c.Close()
	var installed bool
	var err error
	if res.Before, installed, err = c.InstalledVersion(ctx); err != nil {
		return res, wrapErr(err)
	}
	apk, apkErr := FindCompanionAPK()
	res.APK = apk
	if apkErr == nil {
		_, res.Bundled, _ = apkVersion(apk)
	}
	need := !installed || res.Bundled.Code > 0 && res.Before.Code < res.Bundled.Code || res.Before.SignedElsewhere(res.Bundled)
	if !need && force && apkErr == nil {
		local, err := fileSHA256(apk)
		if err != nil {
			return res, err
		}
		remote, err := c.InstalledSHA256(ctx)
		need = err != nil || remote != local
	}
	if need {
		if apkErr != nil {
			return res, fmt.Errorf("%w: %w", apkErr, driver.ErrUnsupported)
		}
		if err := d.installCompanionAPK(ctx, c, apk, res.Before, &res); err != nil {
			return res, err
		}
	}
	return res, d.tryCompanion(ctx)
}

// tryCompanion turns the service on through the driver's own link, as a
// task would, so that a phone that refuses (settings changes over USB)
// says so now; it goes off again a few seconds later unless an agent works.
func (d *Device) tryCompanion(ctx context.Context) error {
	k := &d.comp
	k.mu.Lock()
	defer k.mu.Unlock()
	k.checked, k.retryAt = false, time.Time{}
	if k.on {
		k.evMu.Lock()
		live := k.live
		k.evMu.Unlock()
		if live {
			return nil
		}
		d.companionOffLocked("its event stream ended")
	}
	if err := d.companionOnLocked(ctx); err != nil {
		var cause error = err
		if f := k.failed; f != nil {
			cause = f
		}
		return wrapErr(cause)
	}
	return nil
}

// installCompanionAPK installs the APK, replacing a build signed with
// another key: one known to be (before), or one the install refuses.
func (d *Device) installCompanionAPK(ctx context.Context, c *companion.Client, apk string, before companion.Version, res *CompanionInstall) error {
	_, bundled, _ := apkVersion(apk)
	var err error
	if before.SignedElsewhere(bundled) {
		err = errSignedElsewhere
	} else {
		err = c.Install(ctx, apk)
	}
	if errors.Is(err, errSignedElsewhere) || err != nil && strings.Contains(err.Error(), "INSTALL_FAILED_UPDATE_INCOMPATIBLE") {
		d.cfg.logf("android: %s: the installed companion is signed with another key (%s); replacing it", d.info.ID, signerLine(before.Signer))
		if err := d.replaceCompanion(ctx, c, apk); err != nil {
			return err
		}
		res.Replaced = true
		err = nil
	}
	if err != nil {
		return wrapErr(err)
	}
	res.Installed = true
	d.cfg.logf("android: %s: installed the companion %s (%d)", d.info.ID, res.Bundled.Name, res.Bundled.Code)
	return nil
}

// errSignedElsewhere: the installed build is signed with another key than
// the APK, so `pm install -r` would refuse it.
var errSignedElsewhere = errors.New("the installed companion is signed with another key")

// replaceCompanion removes the installed build and installs the APK. The
// service goes off first, so that Android does not rewrite the person's
// accessibility setting when the package goes; the app's own settings go
// with it, which are only how long it stays on without Pinky.
func (d *Device) replaceCompanion(ctx context.Context, c *companion.Client, apk string) error {
	d.companionOff("replacing the app")
	if err := c.ForceDisable(ctx); err != nil {
		return wrapErr(err)
	}
	if err := c.Uninstall(ctx); err != nil {
		return wrapErr(err)
	}
	if err := c.Install(ctx, apk); err != nil {
		return wrapErr(err)
	}
	k := &d.comp
	k.mu.Lock()
	k.checked, k.retryAt = false, time.Time{}
	k.mu.Unlock()
	return nil
}

// signerLine names a signer for the log.
func signerLine(s string) string {
	switch s {
	case "":
		return "unknown key"
	case companion.OldPublicSigner:
		return "the old public key"
	}
	return "key " + s
}

// companionApp is the build of the companion app this Pinky uses: the one
// it carries (the release build in releases, the development build in
// builds from the source), else the release build.
func companionApp() companion.App {
	apk, err := FindCompanionAPK()
	if err != nil {
		return companion.Release
	}
	app, _, err := apkVersion(apk)
	if err != nil {
		return companion.Release
	}
	return app
}

// apkVersion reads a bundled APK's build and version, cached by path, size
// and time.
func apkVersion(apk string) (companion.App, companion.Version, error) {
	fi, err := os.Stat(apk)
	if err != nil {
		return companion.App{}, companion.Version{}, err
	}
	key := fmt.Sprintf("%s|%d|%d", apk, fi.Size(), fi.ModTime().UnixNano())
	apkVersionMu.Lock()
	defer apkVersionMu.Unlock()
	if v, ok := apkVersions[key]; ok {
		return v.app, v.ver, nil
	}
	app, ver, err := companion.APKVersion(apk)
	if err != nil {
		return app, ver, err
	}
	apkVersions[key] = bundledAPK{app, ver}
	return app, ver, nil
}

type bundledAPK struct {
	app companion.App
	ver companion.Version
}

var (
	apkVersionMu sync.Mutex
	apkVersions  = map[string]bundledAPK{}
)

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

var _ driver.CompanionInstaller = (*Device)(nil)
