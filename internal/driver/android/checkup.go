package android

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/driver/android/adb"
	"github.com/DIYgod/pinky/internal/driver/android/mdns"
)

// A phone's checkup (driver.Checker): the adb server, the companion app, a
// key that does nothing to see that input reaches the phone, a
// screenshot, background displays, Wi-Fi and, on a Mac, the local network.
// It reads and presses nothing the person would notice.

// inputCheckWait is how long the input check watches the scrcpy server's
// log for a refusal after its key: the server logs one at once.
const inputCheckWait = 1200 * time.Millisecond

// localNetworkWait is how long the local network check searches: long
// enough for its first questions to come back (mdns waits 1.5 s for that).
const localNetworkWait = 2500 * time.Millisecond

// Checkup implements driver.Checker.
func (d *Device) Checkup(ctx context.Context) (driver.Checkup, error) {
	out, err := d.client.Shell(ctx, d.info.ID, "getprop")
	if err != nil {
		return driver.Checkup{}, wrapErr(err)
	}
	props := parseProps(string(out))
	cu := driver.Checkup{}
	cu.Family, cu.System = familyOf(props)

	checks := []func(context.Context) driver.Check{
		d.checkADB, d.checkCompanion,
		func(ctx context.Context) driver.Check { return d.checkInput(ctx, cu.Family, props) },
		d.checkScreen,
		func(context.Context) driver.Check { return d.checkBackground() },
		func(context.Context) driver.Check { return d.checkWiFi() },
	}
	if runtime.GOOS == "darwin" {
		checks = append(checks, d.checkLocalNetwork)
	}
	cu.Checks = make([]driver.Check, len(checks))
	var wg sync.WaitGroup
	for i, check := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cu.Checks[i] = check(ctx)
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return driver.Checkup{}, ctx.Err()
	}
	return cu, nil
}

func (d *Device) checkADB(ctx context.Context) driver.Check {
	c := driver.Check{ID: driver.CheckADB, Status: driver.CheckPass}
	v, err := d.client.Version(ctx)
	if err != nil {
		c.Status, c.Detail = driver.CheckFail, err.Error()
		return c
	}
	c.Detail = fmt.Sprintf("adb server version %d", v)
	return c
}

func (d *Device) checkCompanion(ctx context.Context) driver.Check {
	c := driver.Check{ID: driver.CheckCompanion}
	cs, err := d.CompanionState(ctx)
	if err != nil {
		c.Status, c.Detail = driver.CheckFail, err.Error()
		return c
	}
	c.Detail = "the companion app is " + string(cs)
	switch cs {
	case driver.CompanionOn, driver.CompanionReady:
		c.Status = driver.CheckPass
	case driver.CompanionOff:
		c.Status, c.Fix = driver.CheckFail, driver.FixTurnOnCompanion
	case driver.CompanionMissing:
		c.Status, c.Fix = driver.CheckFail, driver.FixInstallCompanion
	case driver.CompanionOutdated:
		c.Status, c.Fix = driver.CheckWarn, driver.FixUpdateCompanion
	default: // this build of Pinky carries no companion app
		c.Status, c.Detail = driver.CheckSkip, "this build of Pinky has no companion app to install"
	}
	return c
}

// checkInput sends KEYCODE_UNKNOWN, which apps and the system ignore,
// through the scrcpy session that carries every tap and key, and watches
// the server's log: a phone that refuses input from the computer (Xiaomi
// without USB debugging (Security settings), ColorOS with permission
// monitoring) makes the server log a SecurityException about the
// INJECT_EVENTS permission at once. Xiaomi's switch sets
// persist.security.adbinput at once but takes effect only after a
// restart: a phone that has it and still refuses needs that restart.
func (d *Device) checkInput(ctx context.Context, family string, props map[string]string) driver.Check {
	c := driver.Check{ID: driver.CheckInput}
	l, err := d.session(ctx)
	if err != nil {
		c.Status, c.Detail = driver.CheckFail, err.Error()
		return c
	}
	if err := l.input(func() error { return l.key(keycodeUnknown, 0) }); err != nil {
		c.Status, c.Detail = driver.CheckFail, err.Error()
		return c
	}
	wctx, cancel := context.WithTimeout(ctx, inputCheckWait)
	defer cancel()
	line, err := l.s.WaitLog(wctx, inputRefused)
	switch {
	case err == nil:
		c.Status, c.Fix, c.Detail = driver.CheckFail, driver.FixInputPermission, serverMessage(line)
		if family == "xiaomi" && props["persist.security.adbinput"] == "1" {
			c.Fix, c.Detail = driver.FixRestart, "persist.security.adbinput is 1, but the phone still refuses input: "+c.Detail
		}
	case errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil:
		c.Status, c.Detail = driver.CheckPass, "a key that does nothing reached the phone"
	default:
		// The session ended while it waited: its last words may say why.
		c.Status, c.Detail = driver.CheckFail, err.Error()
		for _, l := range l.s.Logs() {
			if inputRefused(l) {
				c.Fix, c.Detail = driver.FixInputPermission, serverMessage(l)
				break
			}
		}
	}
	return c
}

// inputRefused matches the scrcpy server's log line for a phone that
// refuses injected input: "java.lang.SecurityException: Injecting input
// events requires the caller (or the source of the instrumentation, if
// any) to have the INJECT_EVENTS permission." (scrcpy's FAQ quotes it for
// Xiaomi phones), and the older "Injecting to another application
// requires INJECT_EVENTS permission".
func inputRefused(line string) bool {
	return strings.Contains(line, "INJECT_EVENTS")
}

// serverMessage strips the "[server] LEVEL: " prefix of a server log line.
func serverMessage(line string) string {
	if i := strings.Index(line, ": "); i >= 0 && strings.HasPrefix(line, "[server]") {
		return line[i+2:]
	}
	return line
}

// checkScreen takes a screenshot, and reports a video encoder that failed
// on this phone, which replaces the live picture with still pictures.
func (d *Device) checkScreen(ctx context.Context) driver.Check {
	c := driver.Check{ID: driver.CheckScreen}
	img, err := d.Screenshot(ctx)
	if err != nil {
		c.Status, c.Fix, c.Detail = driver.CheckFail, driver.FixScreenshot, err.Error()
		return c
	}
	b := img.Bounds()
	c.Status, c.Detail = driver.CheckPass, fmt.Sprintf("a %d×%d screenshot", b.Dx(), b.Dy())
	d.mu.Lock()
	noVideo := d.noVideo
	d.mu.Unlock()
	if noVideo != "" {
		c.Status, c.Fix, c.Detail = driver.CheckWarn, driver.FixVideoEncoder, "the video encoder fails: "+noVideo
	}
	return c
}

// checkBackground says whether agents can work on a background display,
// from what the phone's Android version allows (see background.go). It
// opens no display: on Android 17 that would wake the phone.
func (d *Device) checkBackground() driver.Check {
	c := driver.Check{ID: driver.CheckBackground}
	switch {
	case !d.BackgroundSupported():
		c.Status, c.Fix = driver.CheckSkip, driver.FixNewerAndroid
		c.Detail = "background displays need Android 11 or later; this phone runs Android " + d.info.OSVersion
	case !d.alwaysUnlocked():
		c.Status, c.Fix = driver.CheckWarn, driver.FixUnlockedOnly
		c.Detail = "before Android 13, a background display stops while the phone is locked"
	default:
		c.Status, c.Detail = driver.CheckPass, "a background display keeps working while the phone is locked"
	}
	return c
}

// checkWiFi says whether the phone can be paired over Wi-Fi: Wireless
// debugging came with Android 11.
func (d *Device) checkWiFi() driver.Check {
	c := driver.Check{ID: driver.CheckWiFi}
	switch {
	case d.info.Connection == "wifi":
		c.Status, c.Detail = driver.CheckPass, "connected over Wi-Fi"
	case d.props.SDK != 0 && d.props.SDK < 30:
		c.Status, c.Fix = driver.CheckSkip, driver.FixNewerAndroid
		c.Detail = "pairing over Wi-Fi needs Android 11 or later; this phone runs Android " + d.info.OSVersion
	default:
		c.Status, c.Detail = driver.CheckPass, "Wireless debugging can pair this phone over Wi-Fi"
	}
	return c
}

// checkLocalNetwork searches the network for phones for a moment, as Wi-Fi
// pairing does: macOS drops the traffic of an app it does not allow on the
// local network, without an error, and the search notices.
func (d *Device) checkLocalNetwork(ctx context.Context) driver.Check {
	c := driver.Check{ID: driver.CheckLocalNetwork}
	b, err := d.cfg.Browser.Browse(adb.ServiceConnect)
	if err != nil {
		c.Status, c.Detail = driver.CheckWarn, err.Error()
		return c
	}
	defer b.Close()
	select {
	case <-ctx.Done():
		c.Status, c.Detail = driver.CheckSkip, ctx.Err().Error()
		return c
	case <-time.After(localNetworkWait):
	}
	switch err := b.Err(); {
	case errors.Is(err, mdns.ErrNotAllowed):
		c.Status, c.Fix, c.Detail = driver.CheckFail, driver.FixLocalNetwork, err.Error()
	case err != nil:
		c.Status, c.Detail = driver.CheckWarn, err.Error()
	default:
		c.Status, c.Detail = driver.CheckPass, "Pinky can look for phones on this network"
	}
	return c
}

// propRE matches a line of getprop's output: "[ro.product.brand]: [Xiaomi]".
var propRE = regexp.MustCompile(`^\[([^\]]+)\]: \[(.*)\]$`)

// parseProps reads getprop's output.
func parseProps(out string) map[string]string {
	props := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r", ""), "\n") {
		if m := propRE.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			props[m[1]] = strings.TrimSpace(m[2])
		}
	}
	return props
}

// familyOf tells the maker's system a phone runs from its properties, and
// names it with its version for people to read. The maker alone says
// little: a Xiaomi phone may run LineageOS, whose settings are stock
// Android's.
func familyOf(props map[string]string) (family, system string) {
	maker := strings.ToLower(props["ro.product.manufacturer"])
	brand := strings.ToLower(props["ro.product.brand"])
	magic := props["ro.build.version.magic"]
	switch {
	case props["ro.lineage.build.version"] != "":
		// First: LineageOS keeps the maker's name, not its system.
		return "", join("LineageOS", props["ro.lineage.build.version"])
	case props["ro.mi.os.version.name"] != "":
		// "OS2.0" on HyperOS 2. HyperOS keeps ro.miui.ui.version.name
		// ("V816"), so it goes before MIUI.
		return "xiaomi", join("HyperOS", strings.TrimPrefix(props["ro.mi.os.version.name"], "OS"))
	case props["ro.miui.ui.version.name"] != "":
		return "xiaomi", join("MIUI", miuiVersion(props["ro.miui.ui.version.name"]))
	case props["ro.build.version.oplusrom"] != "" || props["ro.build.version.opporom"] != "" ||
		props["ro.build.version.realmeui"] != "" || props["ro.oxygen.version"] != "":
		v := strings.TrimPrefix(nonEmpty(props["ro.build.version.oplusrom"], props["ro.build.version.opporom"]), "V")
		switch {
		case brand == "realme" || maker == "realme":
			return "oppo", join("realme UI", strings.TrimPrefix(props["ro.build.version.realmeui"], "V"))
		case (brand == "oneplus" || maker == "oneplus") && props["ro.oplus.image.system_ext.area"] != "domestic":
			// OxygenOS 13 and later have no property of their own: a
			// OnePlus phone sold outside China runs it.
			return "oppo", join("OxygenOS", nonEmpty(props["ro.oxygen.version"], v))
		}
		return "oppo", join("ColorOS", v)
	case props["ro.vivo.os.build.display.id"] != "" || props["ro.vivo.os.version"] != "" || maker == "vivo":
		// "OriginOS 5", "Funtouch OS_14"; ro.vivo.os.name says Funtouch
		// on OriginOS too.
		id := strings.TrimSpace(strings.ReplaceAll(props["ro.vivo.os.build.display.id"], "_", " "))
		if l := strings.ToLower(id); !strings.Contains(l, "origin") && !strings.Contains(l, "funtouch") {
			id = ""
		}
		return "vivo", id
	case strings.HasPrefix(magic, "MagicOS_") || strings.HasPrefix(magic, "MagicUI_") || props["msc.config.magic.version"] != "":
		// HONOR since it left Huawei: "MagicOS_9.0.0", "MagicUI_6.1.0".
		name := "MagicOS"
		if strings.HasPrefix(magic, "MagicUI_") {
			name = "Magic UI"
		}
		v := props["msc.config.magic.version"]
		if strings.Contains(magic, "_") {
			v = versionAfter(magic, "_")
		}
		return "honor", join(name, v)
	case props["hw_sc.build.platform.version"] != "" || props["ro.build.version.emui"] != "" || magic != "":
		family := "huawei"
		if maker == "honor" || brand == "honor" {
			family = "honor"
		}
		if v := harmonyOS(props); v != "" {
			return family, join("HarmonyOS", v)
		}
		if magic != "" {
			// HONOR phones made while it was Huawei's: "3.0.1".
			return "honor", join("Magic UI", magic)
		}
		return family, join("EMUI", versionAfter(props["ro.build.version.emui"], "_"))
	case props["ro.build.version.oneui"] != "":
		return "samsung", join("One UI", oneUIVersion(props["ro.build.version.oneui"]))
	case maker == "samsung":
		return "samsung", "One UI"
	case maker == "google" && strings.HasPrefix(strings.ToLower(props["ro.product.model"]), "pixel") && !emulator(props):
		return "pixel", ""
	}
	return "", ""
}

// harmonyOS returns the HarmonyOS version (2 to 4, the ones that run
// Android apps) a Huawei phone runs, or "" for EMUI. hw_sc's version shows
// on EMUI 11 to 15 too, so the version in the build's display id decides:
// "LIO-AL00 4.2.0.120(SP1C00E100R5P4)" is HarmonyOS 4.2, "ICL-L29
// 15.0.0.209(C636E3R1P1)" EMUI 15.
func harmonyOS(props map[string]string) string {
	v := props["hw_sc.build.platform.version"]
	if v == "" {
		return ""
	}
	f := strings.Fields(props["ro.huawei.build.display.id"])
	if len(f) < 2 {
		if props["ro.build.ohos.devicetype"] != "" {
			return v
		}
		return ""
	}
	major, err := strconv.Atoi(strings.SplitN(f[1], ".", 2)[0])
	if err != nil || major < 2 || major > 4 {
		return ""
	}
	return v
}

// emulator reports whether the properties are an Android emulator's.
func emulator(props map[string]string) bool {
	return props["ro.kernel.qemu"] == "1" || props["ro.boot.qemu"] == "1" ||
		strings.HasPrefix(props["ro.product.model"], "sdk_")
}

// miuiVersion turns ro.miui.ui.version.name ("V125", "V14", "V140") into
// the version MIUI shows ("12.5", "14").
func miuiVersion(name string) string {
	v := strings.TrimPrefix(strings.ToUpper(name), "V")
	if _, err := strconv.Atoi(v); err != nil {
		return ""
	}
	switch {
	case len(v) == 3 && v[2] == '0':
		return v[:2]
	case len(v) == 3:
		return v[:2] + "." + v[2:]
	}
	return v
}

// oneUIVersion turns ro.build.version.oneui ("60100") into "6.1".
func oneUIVersion(code string) string {
	n, err := strconv.Atoi(code)
	if err != nil || n < 10000 {
		return ""
	}
	major, minor := n/10000, n/100%100
	return fmt.Sprintf("%d.%d", major, minor)
}

// versionAfter returns what follows sep in s ("EmotionUI_14.2.0" → "14.2.0").
func versionAfter(s, sep string) string {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[i+len(sep):]
	}
	return s
}

func join(name, version string) string {
	if version = strings.TrimSpace(version); version == "" {
		return name
	}
	return name + " " + version
}

func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

var _ driver.Checker = (*Device)(nil)
