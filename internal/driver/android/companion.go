package android

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/driver/android/companion"
	"github.com/DIYgod/pinky/internal/screen"
)

// The companion app (android/companion) gives a 30 ms element tree, events,
// ACTION_SET_TEXT input and an accessibility settle signal. Its
// accessibility service is on only while an agent works on the phone: the
// phone's apps can see an accessibility service that is on, and banking
// apps refuse to sign in next to one that is not from an app store. The
// gateway says when an agent works (driver.WorkWatcher): the driver turns
// the service on as a task starts, so that the task's first read finds it
// ready, and off a few seconds after the task ends or the person takes
// over, or a minute after the agent's last call. While no agent works,
// reads do without it (uiautomator), except for unlocking the phone from
// the computer, and nothing turns it on. Callers that never call
// AgentWorking (androidctl, tests) turn it on at their first read and off
// after a minute without calls. Turning it off restores the setting
// exactly; a service someone else turned on goes off too, unless another
// client still uses it.
//
// Without the app, or when it is too old or does not answer, the driver
// falls back to uiautomator and the keyboard. The two never run together:
// a uiautomator dump unbinds every accessibility service.

const (
	// companionIdle is how long the service stays on without driver calls
	// while an agent works.
	companionIdle = 60 * time.Second
	// companionWarm bounds turning the service on as a task starts.
	companionWarm = 20 * time.Second
	// companionMissingRetry is how long to wait before looking for an app
	// that was not installed.
	companionMissingRetry = 5 * time.Minute
	// companionFailRetry is how long to wait before trying an app that
	// failed or did not answer.
	companionFailRetry = 2 * time.Minute
	// maxPendingEvents caps the toasts and notifications kept between two
	// observations.
	maxPendingEvents = 20
)

// companionRest is how long the service stays on without driver calls once
// no agent works: the task ended, or the person took over. The reads right
// after a task (its recording's last frame) still find it on. A variable
// for tests.
var companionRest = 5 * time.Second

// companionLink is a device's connection to its companion app.
type companionLink struct {
	mu      sync.Mutex // serializes turning the service on and off
	client  *companion.Client
	on      bool      // the service is on for the driver and the subscription runs
	checked bool      // installed was looked up
	have    bool      // the app is installed
	version string    // its version name
	retryAt time.Time // do not try again before this
	reason  string    // why the companion is not used, logged once
	// failed is why the service did not come on the last time the driver
	// tried, for CompanionState; nil once it came on.
	failed  error
	idle    *time.Timer
	lastUse atomic.Int64 // Unix nanoseconds of the last driver call
	sub     *companion.Subscription
	stop    context.CancelFunc // ends the subscription's context

	// gated: the gateway reports when an agent works (AgentWorking), and
	// the service is turned on only while one does; working: one does.
	gated, working atomic.Bool

	info *companion.Info // the app's ping while it is on

	evMu   sync.Mutex
	lastAt map[int]time.Time // arrival of the last window or content event, by display (-1: unknown)
	events []screen.Event    // toasts and notifications since the last Observe
	live   bool              // the subscription is delivering
}

// companionUse is what a caller wants the companion for.
type companionUse int

const (
	// forAgent: an agent's read or input. While no agent works, the
	// service stays off and the caller does without it.
	forAgent companionUse = iota
	// forBackground: a background display, which has no uiautomator to
	// fall back on; it exists only for a task. An installed app is tried
	// again at once instead of after the wait that follows a failure.
	forBackground
	// forUnlock: the person unlocking the phone from the computer, on its
	// lock screen, which reads the PIN pad several times: the service
	// comes on for it and goes off a few seconds after.
	forUnlock
)

// companionClient returns the companion client with its service on, or nil
// when the driver should do without it. It turns the service on at the
// first call after the device opened or went idle, while an agent works.
func (d *Device) companionClient(ctx context.Context) *companion.Client {
	return d.companionClientFor(ctx, forAgent)
}

// companionClientFor is companionClient for a use.
func (d *Device) companionClientFor(ctx context.Context, use companionUse) *companion.Client {
	k := &d.comp
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.on {
		k.evMu.Lock()
		live := k.live
		k.evMu.Unlock()
		if live {
			return k.client
		}
		// The subscription ended: the service stopped or was turned off.
		d.companionOffLocked("its event stream ended")
	}
	if use == forAgent && k.gated.Load() && !k.working.Load() {
		return nil // no agent works on the phone: the service stays off
	}
	if time.Now().Before(k.retryAt) && !(use == forBackground && k.checked && k.have) {
		return nil
	}
	if err := d.companionOnLocked(ctx); err != nil {
		wait := companionFailRetry
		if errors.Is(err, errCompanionMissing) {
			wait = companionMissingRetry
		}
		d.companionUnusableLocked(wait, err.Error())
		return nil
	}
	return k.client
}

// errCompanionMissing: the app is not installed.
var errCompanionMissing = errors.New("its app is not installed")

// companionOnLocked turns the service on, checks that it answers and
// subscribes to its events. A failure to turn it on is kept for
// CompanionState.
func (d *Device) companionOnLocked(ctx context.Context) error {
	k := &d.comp
	if k.client == nil {
		k.client = d.newCompanion()
	}
	if !k.checked {
		version, ok, err := k.client.Installed(ctx)
		if err != nil {
			return fmt.Errorf("cannot tell whether it is installed: %w", err)
		}
		k.checked, k.have, k.version = true, ok, version
	}
	if !k.have {
		k.checked = false // look again after the wait
		return fmt.Errorf("%s: %w", k.client.App().Package, errCompanionMissing)
	}
	start := time.Now()
	if err := k.client.Enable(ctx); err != nil {
		if ctx.Err() == nil {
			k.failed = err
		}
		return fmt.Errorf("its service did not start: %w", err)
	}
	info, err := k.client.Ping(ctx)
	if err == nil && info.Protocol != companion.ProtocolVersion {
		err = fmt.Errorf("it speaks protocol %d, the driver %d", info.Protocol, companion.ProtocolVersion)
	}
	if err == nil {
		err = d.subscribeLocked()
	}
	if err != nil {
		d.disableLocked()
		if ctx.Err() == nil {
			k.failed = err
		}
		return fmt.Errorf("version %s: %w", k.version, err)
	}
	k.on = true
	k.info = info
	k.reason = ""
	k.failed = nil
	d.used()
	if k.idle == nil {
		k.idle = time.AfterFunc(d.companionLimit(), d.companionIdleCheck)
	} else {
		k.idle.Reset(d.companionLimit())
	}
	d.cfg.logf("android: %s: companion %s on in %v", d.info.ID, info.Version, time.Since(start).Round(time.Millisecond))
	return nil
}

// newCompanion returns a client for the build of the companion this Pinky
// carries.
func (d *Device) newCompanion() *companion.Client {
	return companion.NewFor(d.companionRunner(), companionApp())
}

// companionRunner runs the companion's adb commands.
func (d *Device) companionRunner() companion.Runner {
	if d.compRun != nil {
		return d.compRun
	}
	return adbRunner{c: d.client, serial: d.info.ID}
}

// used notes a driver call; a minute without one turns the companion off.
func (d *Device) used() { d.comp.lastUse.Store(time.Now().UnixNano()) }

// companionLimit is how long the service stays on without driver calls.
func (d *Device) companionLimit() time.Duration {
	if k := &d.comp; k.gated.Load() && !k.working.Load() {
		return companionRest
	}
	return companionIdle
}

// companionIdleCheck runs when the idle timer fires: it turns the companion
// off after a minute without driver calls (a few seconds once no agent
// works), or waits for the rest of it.
func (d *Device) companionIdleCheck() {
	k := &d.comp
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.on {
		return
	}
	limit := d.companionLimit()
	if since := time.Since(time.Unix(0, k.lastUse.Load())); since < limit {
		k.idle.Reset(limit - since)
		return
	}
	why := "a minute without driver calls"
	if limit == companionRest {
		why = "no agent works on the phone"
	}
	d.companionOffLocked(why)
}

// AgentWorking implements driver.WorkWatcher. When an agent starts working
// it turns the service on in the background, so that the task's first
// read finds it ready; when the agent stops, it turns it off a few seconds
// after the last call. The first call also turns off a service that was
// left on with nobody using it.
func (d *Device) AgentWorking(on bool) {
	k := &d.comp
	first := !k.gated.Swap(true)
	if k.working.Swap(on) == on && !first {
		return
	}
	if on {
		go d.warmCompanion()
	} else {
		go d.restCompanion(first)
	}
}

// warmCompanion turns the service on as an agent starts working.
func (d *Device) warmCompanion() {
	ctx, cancel := context.WithTimeout(context.Background(), companionWarm)
	defer cancel()
	if d.comp.working.Load() {
		d.companionClient(ctx)
	}
}

// restCompanion shortens the service's time without calls once no agent
// works and, at the first call, turns off a service left on before.
func (d *Device) restCompanion(first bool) {
	k := &d.comp
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.working.Load() {
		return
	}
	switch {
	case k.on:
		if k.idle != nil {
			k.idle.Reset(companionRest) // measured from the last call
		}
	case first:
		d.quietLocked()
	}
}

// quietLocked turns the service off when it is on although no agent works
// and no client uses it: Pinky until 2026-11-08 kept it on for good once
// installed. One that another client uses, or that the phone does not let
// the computer turn off, stays on.
func (d *Device) quietLocked() {
	k := &d.comp
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if time.Now().Before(k.retryAt) {
		return
	}
	if k.client == nil {
		k.client = d.newCompanion()
	}
	listed, err := k.client.Enabled(ctx)
	if err != nil || !listed {
		return
	}
	if others := companionPeers(ctx, k.client); others {
		d.cfg.logf("android: %s: another client is using the companion; leaving its service on", d.info.ID)
		return
	}
	if err := k.client.ForceDisable(ctx); err != nil {
		d.cfg.logf("android: %s: turning off the companion's service, which was on with no agent working: %v", d.info.ID, err)
		return
	}
	d.cfg.logf("android: %s: turned off the companion's service, which was on with no agent working; Pinky turns it on only while an agent works", d.info.ID)
}

// closeCompanion turns the companion off for good, when the device closes.
func (d *Device) closeCompanion() {
	k := &d.comp
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.on {
		d.companionOffLocked("the device closed")
	} else if k.client != nil {
		k.client.Close()
	}
	k.retryAt = time.Now().Add(100 * 365 * 24 * time.Hour)
}

// subscribeLocked starts the event subscription, which outlives the call
// that started it.
func (d *Device) subscribeLocked() error {
	k := &d.comp
	ctx, stop := context.WithCancel(context.Background())
	sub, err := k.client.Events(ctx,
		companion.EventWindow, companion.EventWindows, companion.EventContent,
		companion.EventToast, companion.EventNotification)
	if err != nil {
		stop()
		return fmt.Errorf("subscribe to events: %w", err)
	}
	k.sub, k.stop = sub, stop
	k.evMu.Lock()
	k.live = true
	k.events = nil
	k.lastAt = map[int]time.Time{}
	k.evMu.Unlock()
	go d.readCompanionEvents(sub)
	return nil
}

func (d *Device) readCompanionEvents(sub *companion.Subscription) {
	k := &d.comp
	for ev := range sub.C() {
		now := time.Now()
		k.evMu.Lock()
		switch ev.Kind {
		case companion.EventWindow, companion.EventWindows, companion.EventContent:
			k.lastAt[ev.Display] = now
		case companion.EventDropped:
			k.lastAt[-1] = now
		}
		if se, ok := ev.ScreenEvent(); ok {
			k.events = append(k.events, se)
			if len(k.events) > maxPendingEvents {
				k.events = k.events[len(k.events)-maxPendingEvents:]
			}
		}
		k.evMu.Unlock()
	}
	k.evMu.Lock()
	if k.sub == sub {
		k.live = false
	}
	k.evMu.Unlock()
}

// takeEvents returns the toasts and notifications since the last call.
func (k *companionLink) takeEvents() []screen.Event {
	k.evMu.Lock()
	defer k.evMu.Unlock()
	ev := k.events
	k.events = nil
	return ev
}

// lastChange is the arrival of the last window or content event on a
// display, or of one whose display is not known; ok is false when no
// subscription is delivering. Apps from before round 3 send no display,
// which reads as 0, the main display.
func (k *companionLink) lastChange(display int) (time.Time, bool) {
	k.evMu.Lock()
	defer k.evMu.Unlock()
	last := k.lastAt[display]
	if u := k.lastAt[-1]; u.After(last) {
		last = u
	}
	return last, k.live
}

// displays reports whether the app on the phone takes displays other than
// the main one for set_text (Android 11 and later, apps from round 3).
func (k *companionLink) displays() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.on && k.info != nil && k.info.Has(companion.FeatureDisplays)
}

// settleCond is the companion's settle condition for a display, or false
// when no subscription is delivering.
func (d *Device) settleCond(display int) (quietCond, bool) {
	if _, live := d.comp.lastChange(display); !live {
		return quietCond{}, false
	}
	return quietCond{
		last: func() time.Time {
			last, live := d.comp.lastChange(display)
			if !live {
				return time.Time{} // the subscription ended: video alone
			}
			return last
		},
		quiet: companion.DefaultQuiet,
	}, true
}

// companionUnusableLocked records why the companion is not used and when
// to try again, logging each new reason once.
func (d *Device) companionUnusableLocked(wait time.Duration, reason string) {
	k := &d.comp
	k.retryAt = time.Now().Add(wait)
	if reason != k.reason {
		k.reason = reason
		d.cfg.logf("android: %s: not using the companion app: %s; using uiautomator", d.info.ID, reason)
	}
}

// companionFailed turns the companion off after a call failed, so that
// uiautomator can run, and waits before trying it again.
func (d *Device) companionFailed(err error) {
	k := &d.comp
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.on {
		d.companionOffLocked(fmt.Sprintf("a request failed: %v", err))
	}
	d.companionUnusableLocked(companionFailRetry, fmt.Sprintf("a request failed: %v", err))
}

// companionOff turns the companion off, if it is on.
func (d *Device) companionOff(why string) {
	k := &d.comp
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.on {
		d.companionOffLocked(why)
	}
}

func (d *Device) companionOffLocked(why string) {
	k := &d.comp
	d.disableLocked()
	k.on = false
	k.info = nil
	d.cfg.logf("android: %s: companion off: %s", d.info.ID, why)
}

// disableLocked ends the subscription and turns the service off: exactly
// as Enable found the setting when the driver turned it on, or by removing
// only Pinky's entry when it was on already (an older Pinky kept it on for
// good, or another client turned it on for its reads). One that another
// client still uses stays on: several drivers (two Pinky processes, Pinky
// and androidctl) may share it, and the last to leave turns it off.
func (d *Device) disableLocked() {
	k := &d.comp
	if k.idle != nil {
		k.idle.Stop()
	}
	if k.sub != nil {
		k.sub.Close()
		k.sub = nil
	}
	if k.stop != nil {
		k.stop()
		k.stop = nil
	}
	k.evMu.Lock()
	k.live = false
	k.evMu.Unlock()
	if k.client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var err error
	switch {
	case companionPeers(ctx, k.client):
		d.cfg.logf("android: %s: another client is using the companion; leaving its service on", d.info.ID)
	case k.client.EnabledForTask():
		err = k.client.Disable(ctx)
	default:
		err = k.client.ForceDisable(ctx)
	}
	if err != nil {
		d.cfg.logf("android: %s: turning the companion off: %v", d.info.ID, err)
	}
	k.client.Close()
}

// companionPeers asks the app whether clients other than this one's
// request connection are connected. The event subscription was just
// closed, and the app may count it for a moment, so it asks a few times.
// When the app does not answer, there is nobody to cut off.
func companionPeers(ctx context.Context, c *companion.Client) bool {
	for i := 0; i < 3; i++ {
		if i > 0 {
			if sleepCtx(ctx, 100*time.Millisecond) != nil {
				return false
			}
		}
		pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		info, err := c.Ping(pctx)
		cancel()
		if err != nil || info.Connections <= 1 {
			return false
		}
	}
	return true
}

// companionInUse reports whether the driver has the service on.
func (d *Device) companionInUse() bool {
	k := &d.comp
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.on
}

// companionFailure is why the service did not come on the last time the
// driver tried; nil when it did, or before any try.
func (d *Device) companionFailure() error {
	k := &d.comp
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.failed
}

var _ driver.WorkWatcher = (*Device)(nil)

// observeCompanion reads the screen from the companion, with the lock and
// keyboard state from the window manager as the uiautomator path has them.
// The two dumpsys queries run as separate shells, in parallel with the
// tree: in one shell they took about 85 ms, the tree about 30.
func (d *Device) observeCompanion(ctx context.Context, c *companion.Client) (*screen.Screen, error) {
	run := func(cmd string) <-chan string {
		ch := make(chan string, 1)
		go func() {
			out, err := d.client.Shell(ctx, d.info.ID, cmd)
			if err != nil {
				d.cfg.logf("android: %s: read state: %v", d.info.ID, err)
			}
			ch <- string(out)
		}()
		return ch
	}
	keyguardc, imec := run(keyguardCmd), run(imeWindowCmd)
	t, err := c.Dump(ctx, companion.DumpOptions{})
	keyguard, ime := <-keyguardc, <-imec
	if err != nil {
		return nil, err
	}
	s := t.Screen(companion.ScreenOptions{})
	s.CapturedAt = c.HostTime(t.Time)
	if locked, ok := parseKeyguard(keyguard); ok {
		s.Locked = locked
	}
	if imeWindowRE.MatchString(ime) {
		s.Keyboard = parseIMEWindowVisible(ime)
	}
	if emptyWithKeyboard(s) {
		// The keyboard covers the app with a screen of its own (Gboard's
		// stylus tutorial, setup or a full-screen panel), whose nodes a
		// normal read leaves out: read them too.
		if t2, err := c.Dump(ctx, companion.DumpOptions{IME: true}); err == nil {
			if s2 := t2.Screen(companion.ScreenOptions{Keyboard: true}); len(s2.Elements) > 0 {
				s2.CapturedAt = c.HostTime(t2.Time)
				s2.Locked, s2.Keyboard = s.Locked, s.Keyboard
				s = s2
			}
		}
	}
	s.App.Name = d.companionAppName(s.App)
	s.Events = d.comp.takeEvents()
	return s, nil
}

// companionAppName names the foreground app from the driver's app list,
// as the uiautomator path does, so headers stay the same across the two.
// The companion's own name may be a window title on the first tree after
// an app opens. The list loads in the background the first time.
func (d *Device) companionAppName(app screen.App) string {
	if name := d.appName(app.ID); name != "" {
		return name
	}
	d.appsMu.Lock()
	loaded := d.apps != nil
	d.appsMu.Unlock()
	if !loaded && d.appsLoading.CompareAndSwap(false, true) {
		go func() {
			defer d.appsLoading.Store(false)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, _ = d.cachedApps(ctx, 0)
		}()
	}
	return app.Name
}

// refusedText reports whether the companion refused a set_text request
// before changing anything, so typing it another way cannot duplicate it.
func refusedText(err error) bool {
	var ce *companion.Error
	if !errors.As(err, &ce) {
		return false
	}
	switch ce.Code {
	case "not_found", "not_editable", "disabled", "unsupported", "action_failed":
		return true
	}
	return false
}

// typeCompanion writes text with the companion into the focused field of
// a display, or else into the field under the last tap there (tap
// returns it), and presses Enter with enter when Submit was asked and the
// companion could not submit. It returns done=false when the companion
// refused and the caller should type another way.
func (d *Device) typeCompanion(ctx context.Context, c *companion.Client, text string, append, submit bool, display int, tap func() (screen.Point, bool), enter func() error) (bool, error) {
	o := companion.SetTextOptions{Append: append, Submit: submit, Display: display}
	res, err := c.SetText(ctx, companion.Target{}, text, o)
	if refusedText(err) {
		if p, ok := tap(); ok {
			res, err = c.SetText(ctx, companion.Target{Point: &p}, text, o)
		}
	}
	switch {
	case err == nil:
		if submit && !res.Submitted {
			return true, enter()
		}
		return true, nil
	case refusedText(err):
		d.cfg.logf("android: %s: the companion could not set the text (%v); typing it instead", d.info.ID, err)
		return false, nil
	default:
		// The request may have reached the phone; typing the text again
		// could enter it twice, so report it instead.
		d.companionFailed(err)
		return true, fmt.Errorf("android: set text through the companion: %w", err)
	}
}

// recentTap returns the last tap's point if it was in the last minute.
func (d *Device) recentTap() (screen.Point, bool) {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	if d.tapAt.IsZero() || time.Since(d.tapAt) > time.Minute {
		return screen.Point{}, false
	}
	return d.tapPoint, true
}
