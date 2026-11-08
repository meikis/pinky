package fakeapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/gateway"
)

// Checkups. A fake phone's checkup starts with the gateway's connection
// items; an online Android phone then gets items drawn from what the fake
// knows of it (its companion app), or the ones SetCheckup gave it.

// checkupDelay is how long a fake checkup takes at Speed 1.
const checkupDelay = 1200 * time.Millisecond

// Checkup runs a fake phone's checkup.
func (a *API) Checkup(ctx context.Context, deviceID string) (gateway.CheckupView, error) {
	a.mu.Lock()
	d := a.device(deviceID)
	if d == nil {
		a.mu.Unlock()
		return gateway.CheckupView{}, fmt.Errorf("fakeapi: no device %q: %w", deviceID, driver.ErrNotFound)
	}
	info, comp, set := d.info, d.companion, d.checkup
	a.mu.Unlock()
	select {
	case <-ctx.Done():
		return gateway.CheckupView{}, ctx.Err()
	case <-time.After(time.Duration(float64(checkupDelay) / a.opt.Speed)):
	}
	v := gateway.CheckupView{DeviceID: info.ID, Platform: info.Platform, At: a.opt.Now(), Checks: gateway.ConnectionChecks(info)}
	if info.State != driver.StateOnline {
		return v, nil
	}
	cu := driver.Checkup{}
	switch {
	case set != nil:
		cu = *set
		cu.Checks = append([]driver.Check(nil), set.Checks...)
	case info.Platform == driver.Android:
		cu = defaultCheckup(info)
	}
	// The companion item follows the fake's app, which installs change.
	for i, c := range cu.Checks {
		if c.ID == driver.CheckCompanion {
			cu.Checks[i] = companionCheck(comp)
		}
	}
	v.Family, v.System = cu.Family, cu.System
	v.Checks = append(v.Checks, cu.Checks...)
	return v, nil
}

// SetCheckup sets what a fake phone's own checkup finds. A companion item
// in it follows the fake's companion app.
func (a *API) SetCheckup(deviceID string, cu driver.Checkup) {
	a.mu.Lock()
	if d := a.device(deviceID); d != nil {
		d.checkup = &cu
	}
	a.mu.Unlock()
}

// SetInstallError makes InstallCompanion fail on the phone with err, as a
// phone that blocks installs over USB does; nil lets it install again.
func (a *API) SetInstallError(deviceID string, err error) {
	a.mu.Lock()
	if d := a.device(deviceID); d != nil {
		d.installErr = err
	}
	a.mu.Unlock()
}

// defaultCheckup is what an Android phone the fake knows nothing more of
// reports: every item works, as on a Pixel.
func defaultCheckup(info driver.Info) driver.Checkup {
	cu := driver.Checkup{Family: "pixel"}
	if strings.HasPrefix(info.Model, "Galaxy") {
		cu.Family, cu.System = "samsung", "One UI 8.0"
	}
	cu.Checks = []driver.Check{
		{ID: driver.CheckADB, Status: driver.CheckPass, Detail: "adb server version 41"},
		{ID: driver.CheckCompanion},
		{ID: driver.CheckInput, Status: driver.CheckPass, Detail: "a key that does nothing reached the phone"},
		{ID: driver.CheckScreen, Status: driver.CheckPass, Detail: "a 1080×2400 screenshot"},
		{ID: driver.CheckBackground, Status: driver.CheckPass, Detail: "a background display keeps working while the phone is locked"},
		{ID: driver.CheckWiFi, Status: driver.CheckPass, Detail: "Wireless debugging can pair this phone over Wi-Fi"},
	}
	return cu
}

// companionCheck is the companion item for the fake's app.
func companionCheck(cs driver.CompanionState) driver.Check {
	c := driver.Check{ID: driver.CheckCompanion, Detail: "the companion app is " + string(cs)}
	switch cs {
	case "", driver.CompanionOn, driver.CompanionReady:
		c.Status, c.Detail = driver.CheckPass, "the companion app is "+string(cs)
	case driver.CompanionOff:
		c.Status, c.Fix = driver.CheckFail, driver.FixTurnOnCompanion
	case driver.CompanionMissing:
		c.Status, c.Fix = driver.CheckFail, driver.FixInstallCompanion
	case driver.CompanionOutdated:
		c.Status, c.Fix = driver.CheckWarn, driver.FixUpdateCompanion
	default:
		c.Status = driver.CheckSkip
	}
	return c
}

// XiaomiCheckup is what a Redmi phone on HyperOS reports while USB
// debugging (Security settings) is off: it refuses input, and its
// companion app is not installed (Install via USB is off too).
func XiaomiCheckup() driver.Checkup {
	return driver.Checkup{Family: "xiaomi", System: "HyperOS 2.0", Checks: []driver.Check{
		{ID: driver.CheckADB, Status: driver.CheckPass, Detail: "adb server version 41"},
		{ID: driver.CheckCompanion},
		{ID: driver.CheckInput, Status: driver.CheckFail, Fix: driver.FixInputPermission,
			Detail: "java.lang.SecurityException: Injecting input events requires the caller (or the source of the instrumentation, if any) to have the INJECT_EVENTS permission."},
		{ID: driver.CheckScreen, Status: driver.CheckPass, Detail: "a 1220×2712 screenshot"},
		{ID: driver.CheckBackground, Status: driver.CheckPass, Detail: "a background display keeps working while the phone is locked"},
		{ID: driver.CheckWiFi, Status: driver.CheckPass, Detail: "Wireless debugging can pair this phone over Wi-Fi"},
	}}
}

// errRestricted is the error a Xiaomi phone gives an install over USB while
// Install via USB is off.
var errRestricted = errors.New("android: install pinky-companion.apk: Failure [INSTALL_FAILED_USER_RESTRICTED: Install canceled by user]")

// Unplug takes a phone offline, as when its cable comes out.
func (a *API) Unplug(deviceID string) {
	a.mu.Lock()
	d := a.device(deviceID)
	if d != nil {
		d.info.State, d.info.Detail = driver.StateOffline, ""
	}
	a.mu.Unlock()
	a.emit(gateway.Event{Kind: gateway.EventDevices, DeviceID: deviceID})
}
