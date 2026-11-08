package gateway

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/DIYgod/pinky/internal/approval"
	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/history"
	"github.com/DIYgod/pinky/internal/policy"
	"github.com/DIYgod/pinky/internal/screen"
)

// This file implements API, the client side: the desktop app and remote
// consoles. Agents never reach these methods.

// Errors the client API returns.
var (
	ErrUnknownDevice = errors.New("unknown device")
	ErrUnknownNeed   = errors.New("unknown or finished request")
	ErrNoUnlock      = errors.New("unlocking from the computer is not available; unlock on the phone")
)

// unlockNeedPrefix starts the ids of unlock cards: "unlock:<device id>".
const unlockNeedPrefix = "unlock:"

// Devices implements API.
func (g *Gateway) Devices() []DeviceView {
	g.mu.Lock()
	defer g.mu.Unlock()
	open := g.approvals.List(false)
	var out []DeviceView
	for _, d := range g.sortedDevicesLocked() {
		info := d.info
		if !d.online && info.State == driver.StateOnline {
			info.State = driver.StateOffline
		}
		st, line := g.statusLocked(d, open)
		v := DeviceView{Info: info, Status: st, StatusLine: line}
		if d.online && d.dev != nil {
			v.Features = featuresOf(d.dev)
		}
		if d.lease != nil {
			v.TaskID = d.lease.task.id
		}
		out = append(out, v)
	}
	return out
}

// backgroundWorks reports whether agents can work on a background display
// of the phone.
func backgroundWorks(dev driver.Device) bool {
	if _, ok := dev.(driver.Backgrounder); !ok {
		return false
	}
	if bs, ok := dev.(driver.BackgroundSupporter); ok {
		return bs.BackgroundSupported()
	}
	return true
}

func featuresOf(dev driver.Device) Features {
	var f Features
	_, f.Unlock = dev.(driver.Unlocker)
	f.Background = backgroundWorks(dev)
	_, f.Files = dev.(driver.Files)
	_, f.Companion = dev.(driver.CompanionInstaller)
	return f
}

func (g *Gateway) statusLocked(d *device, open []approval.Info) (DeviceStatus, string) {
	l := d.lease
	if !d.online {
		line := "Offline"
		if d.info.Detail != "" && d.info.State != driver.StateOnline {
			line += " · " + d.info.Detail
		}
		if l != nil {
			line = "Offline · reconnecting for " + l.task.title
		}
		return DeviceOffline, line
	}
	for _, info := range open {
		if info.Binding.Device == d.id {
			if info.Kind == approval.KindHandoff {
				return DeviceNeedsYou, "Needs you · " + info.Title
			}
			return DeviceNeedsYou, "Waiting for you · " + info.Title
		}
	}
	if d.locked && (l == nil || l.bg == nil) {
		if l != nil {
			return DeviceNeedsYou, "Locked · unlock to continue " + l.task.title
		}
		return DeviceLocked, "Locked"
	}
	if d.person {
		if l != nil {
			return DeviceInControl, "You're in control · " + l.session.agent.Name + " is paused"
		}
		return DeviceInControl, "You're in control"
	}
	if l != nil {
		line := l.session.agent.Name + " · " + l.task.title
		if l.bg != nil {
			line += " · in the background"
		}
		return DeviceWorking, line
	}
	return DeviceIdle, "Idle"
}

// Tasks implements API: running tasks, then tasks that ended in the last
// ten minutes.
func (g *Gateway) Tasks() []TaskView {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []TaskView
	for _, d := range g.sortedDevicesLocked() {
		if d.lease != nil {
			out = append(out, taskView(d.lease.task))
		}
	}
	cutoff := time.Now().Add(-10 * time.Minute)
	for i := len(g.ended) - 1; i >= 0; i-- {
		if t := g.ended[i]; t.ended.After(cutoff) {
			out = append(out, taskView(t))
		}
	}
	return out
}

func taskView(t *task) TaskView {
	return TaskView{ID: t.id, DeviceID: t.deviceID, DeviceName: t.deviceName, Agent: t.agent, Title: t.title,
		State: t.state, Started: t.started, Ended: t.ended, Steps: append([]StepView(nil), t.steps...), ReadOnly: t.readOnly}
}

// Needs implements API.
func (g *Gateway) Needs() []NeedView {
	var out []NeedView
	for _, info := range g.approvals.List(false) {
		kind := NeedApproval
		if info.Kind == approval.KindHandoff {
			kind = NeedHandoff
		}
		out = append(out, NeedView{ID: info.ID, Kind: kind, DeviceID: info.Binding.Device, DeviceName: info.DeviceName,
			App: info.App, Agent: info.Agent, TaskID: info.TaskID, Title: info.Title, Detail: info.Detail, Rule: info.Rule,
			Created: info.Created, Deadline: info.Deadline, Screenshot: info.Screenshot, Target: info.Target,
			Point: info.Point, To: info.To, AlwaysAllow: g.canAlwaysAllow(info), AllowTask: canAllowTask(info),
			Unblock: isUnblock(info), ReadOnly: askedReadOnly(info)})
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := range out {
		if out[i].Kind != NeedHandoff {
			continue
		}
		if d := g.devices[out[i].DeviceID]; d != nil && d.lease != nil {
			for _, id := range d.lease.unlockHandoffs {
				out[i].EndsOnUnlock = out[i].EndsOnUnlock || id == out[i].ID
			}
		}
	}
	for _, d := range g.sortedDevicesLocked() {
		if l := d.lease; l != nil && d.locked && d.online && l.bg == nil {
			out = append(out, NeedView{ID: unlockNeedPrefix + d.id, Kind: NeedUnlock, DeviceID: d.id, DeviceName: d.name(),
				Agent: l.session.agent.Name, TaskID: l.task.id, Title: "Unlock " + d.name(),
				Detail:  fmt.Sprintf("%s is waiting for the phone to unlock · %s", l.session.agent.Name, l.task.title),
				Created: time.Now()})
		}
	}
	return out
}

// History implements API.
func (g *Gateway) History(ctx context.Context, q HistoryQuery) ([]TaskView, error) {
	if g.store == nil {
		return g.memoryHistory(q), nil
	}
	g.rec.flush()
	tasks, err := g.store.Tasks(ctx, history.Query{Search: q.Search, Before: q.Before, Limit: q.Limit})
	if err != nil {
		return nil, err
	}
	out := make([]TaskView, 0, len(tasks))
	for _, t := range tasks {
		steps, err := g.store.Steps(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		v := TaskView{ID: t.ID, DeviceID: t.DeviceID, DeviceName: t.DeviceName, Agent: t.Agent, Title: t.Title,
			State: TaskState(t.State), Started: t.Started, Ended: t.Ended}
		for _, st := range steps {
			v.Steps = append(v.Steps, StepView{Index: st.Index, Text: st.Text, Tool: st.Tool, Args: st.Args, Status: st.Status,
				At: st.Started, Elapsed: st.Elapsed, Before: st.Before, After: st.After, Target: st.Target, Point: st.Point})
		}
		out = append(out, v)
	}
	return out, nil
}

func (g *Gateway) memoryHistory(q HistoryQuery) []TaskView {
	g.mu.Lock()
	defer g.mu.Unlock()
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	var all []*task
	for _, d := range g.devices {
		if d.lease != nil {
			all = append(all, d.lease.task)
		}
	}
	all = append(all, g.ended...)
	var out []TaskView
	search := strings.ToLower(q.Search)
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		t := all[i]
		if !q.Before.IsZero() && !t.started.Before(q.Before) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(t.title+" "+t.agent+" "+t.deviceName), search) {
			continue
		}
		out = append(out, taskView(t))
	}
	return out
}

// Decide implements API. It is the only way to answer an approval.
func (g *Gateway) Decide(needID string, d Decision, note string) error {
	if dev, ok := strings.CutPrefix(needID, unlockNeedPrefix); ok {
		if d == Deny {
			return nil
		}
		return g.Unlock(dev)
	}
	info, err := g.approvals.Get(needID)
	if err != nil {
		return ErrUnknownNeed
	}
	dec := approval.Decision(d)
	if !dec.Valid() {
		return fmt.Errorf("unknown decision %q", d)
	}
	switch info.Kind {
	case approval.KindHandoff:
		switch d {
		case TakeOver:
			return g.TakeOver(info.Binding.Device)
		case Deny:
			return g.decide(needID, approval.Deny, note)
		}
		return g.HandBack(info.Binding.Device, note)
	case approval.KindAgent:
		if d == TakeOver {
			dec = approval.Deny
		}
		return g.decide(needID, dec, note)
	}
	if d == TakeOver {
		// The panel's Take over: the person does it themselves.
		return g.TakeOver(info.Binding.Device)
	}
	// An answer counts for what the approval can keep, so the agent and
	// the history hear what it did: Always allow that cannot be kept as
	// Allow for this task, and Allow for this task where nothing lasts for
	// the task as Allow once. Older clients offer both on the agent's own
	// questions (NeedView.AlwaysAllow, NeedView.AllowTask).
	var kept error
	if dec == approval.AllowAlways {
		can := g.canAlwaysAllow(info)
		// Kept before the answer wakes the agent's call, which then finds
		// the action no longer asks (always.go). Where it cannot be kept,
		// alwaysAllow keeps nothing, and says why only when rules.yaml has
		// an edit that cannot be used: the panel may have offered Always
		// allow before the file broke.
		if info.State == approval.Open {
			kept = g.alwaysAllow(info)
		}
		if !can {
			dec = approval.AllowTask
		}
	}
	if dec == approval.AllowTask && !canAllowTask(info) {
		dec = approval.AllowOnce
	}
	if err := g.decide(needID, dec, note); err != nil {
		return err
	}
	return kept
}

// canAllowTask reports whether an approval's Allow for this task lasts:
// for an action the policy asked about, the rules that asked no longer ask
// in its app for the rest of the task (awaitApproval), and once copying
// files is allowed, later copies do not ask (askToPush). An agent's own
// question gets a yes for that question only: each one is the person's
// consent to what it says, and answering the next ones by itself would let
// the agent claim consent the person never gave.
func canAllowTask(info approval.Info) bool {
	if info.Kind != approval.KindAction {
		return false
	}
	if snap, ok := info.Data.(*snapshot); ok {
		return len(snap.asks) > 0
	}
	return info.Rule == filesRule
}

func (g *Gateway) decide(id string, d approval.Decision, note string) error {
	if err := g.approvals.Decide(id, d, note); err != nil {
		return ErrUnknownNeed
	}
	return nil
}

func (g *Gateway) device(id string) (*device, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	d, ok := g.devices[id]
	if !ok {
		return nil, ErrUnknownDevice
	}
	return d, nil
}

// TakeOver implements API: the agent pauses and its next call returns
// user_took_over. Open approvals on the device are withdrawn.
func (g *Gateway) TakeOver(deviceID string) error {
	d, err := g.device(deviceID)
	if err != nil {
		return err
	}
	g.takeOver(d, false)
	return nil
}

// takeOver puts the person in control. Input on the mirror (byInput) does
// so only while an agent task runs; without one it is ordinary use. The
// Take over button always does, and control stays with the person until
// HandBack, across leases.
func (g *Gateway) takeOver(d *device, byInput bool) {
	g.mu.Lock()
	if l := d.lease; byInput && l != nil && l.bg != nil {
		// The person uses the phone's own screen; the agent works on its
		// background display and goes on.
		d.personAt = time.Now()
		g.mu.Unlock()
		return
	}
	// The person may change the screen from now on, task or not.
	g.inputLocked(d)
	g.pauseVideoLocked(d)
	l := d.lease
	if l == nil && byInput {
		g.mu.Unlock()
		return
	}
	changed := !d.person
	if changed {
		d.person = true
		g.noteWorkLocked(d)
		if l != nil {
			l.tookOverSeen = false
			l.handedBack, l.handBackNote = false, ""
			l.task.state = TaskPaused
			g.recordTaskLocked(l.task)
		}
		g.wakeLocked(d)
	}
	if byInput {
		d.personInput = true
	}
	if d.inflight != nil {
		d.inflight()
	}
	g.mu.Unlock()
	// Withdraw open approvals even when the person was already in control:
	// one may have opened in the meantime.
	gone := g.approvals.WithdrawWhere(func(i approval.Info) bool {
		return i.Binding.Device == d.id && i.Kind != approval.KindHandoff
	}, reasonTookOver)
	g.closePendingSteps(gone, string(StatusTookOver))
	if changed {
		g.emit(Event{Kind: EventDevices, DeviceID: d.id})
		if l != nil {
			g.emit(Event{Kind: EventTasks, DeviceID: d.id, TaskID: l.task.id})
		}
	}
}

// HandBack implements API: control returns to the agent, and a waiting
// phone_handoff returns with the note.
func (g *Gateway) HandBack(deviceID, note string) error {
	d, err := g.device(deviceID)
	if err != nil {
		return err
	}
	g.mu.Lock()
	d.person, d.personInput = false, false
	g.noteWorkLocked(d)
	l := d.lease
	if l == nil {
		g.wakeLocked(d)
		g.mu.Unlock()
		g.emit(Event{Kind: EventDevices, DeviceID: d.id})
		return nil
	}
	if l.task.ended.IsZero() {
		l.task.state = TaskWorking
		g.recordTaskLocked(l.task)
	}
	var handoffs []string
	for _, info := range g.approvals.List(false) {
		if info.Binding.Lease == l.id && info.Kind == approval.KindHandoff {
			handoffs = append(handoffs, info.ID)
		}
	}
	if len(handoffs) == 0 {
		l.handedBack, l.handBackNote = true, note
	}
	g.wakeLocked(d)
	g.mu.Unlock()
	for _, id := range handoffs {
		_ = g.approvals.Decide(id, approval.AllowOnce, note)
	}
	g.emit(Event{Kind: EventDevices, DeviceID: d.id})
	g.emit(Event{Kind: EventTasks, DeviceID: d.id, TaskID: l.task.id})
	return nil
}

// Stop implements API: the device's lease ends, its pending actions are
// cancelled, and the session may not use the device again.
func (g *Gateway) Stop(deviceID string) error {
	d, err := g.device(deviceID)
	if err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if d.lease != nil {
		g.endLeaseLocked(d.lease, OutcomeStopped, "")
	}
	return nil
}

// StopAll implements API: the emergency stop.
func (g *Gateway) StopAll() error {
	g.mu.Lock()
	for _, d := range g.devices {
		if d.lease != nil {
			d.lease.session.stopped = true
			g.endLeaseLocked(d.lease, OutcomeStopped, "")
		}
	}
	g.mu.Unlock()
	gone := g.approvals.WithdrawWhere(func(approval.Info) bool { return true }, OutcomeStopped)
	g.closePendingSteps(gone, OutcomeStopped)
	return nil
}

// Stream implements API: the live video of the phone's main screen. The
// mirror's viewers and the recorder share one stream from the phone (see
// streams.go).
func (g *Gateway) Stream(ctx context.Context, deviceID string, opt driver.StreamOptions) (driver.Stream, error) {
	d, err := g.device(deviceID)
	if err != nil {
		return nil, err
	}
	dev := g.driverOf(d)
	if dev == nil {
		return nil, driver.ErrOffline
	}
	return g.watch(ctx, hubKey(d.id, false), dev, opt)
}

// Touch implements API. The person's input pauses the agent.
func (g *Gateway) Touch(deviceID string, a driver.TouchAction, p screen.Point) error {
	d, dev, err := g.personInput(deviceID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(g.ctx, 10*time.Second)
	defer cancel()
	if t, ok := dev.(driver.Toucher); ok {
		return t.Touch(ctx, a, p)
	}
	// Without raw touches, turn down and up into a tap or a swipe.
	g.mu.Lock()
	start := d.touch
	switch a {
	case driver.TouchDown:
		d.touch = &touchState{from: p, at: time.Now()}
	case driver.TouchUp:
		d.touch = nil
	}
	g.mu.Unlock()
	if a != driver.TouchUp {
		return nil
	}
	if start == nil {
		return dev.Tap(ctx, p, driver.TapOptions{})
	}
	held := time.Since(start.at)
	if math.Hypot(float64(p.X-start.from.X), float64(p.Y-start.from.Y)) < 24 {
		return dev.Tap(ctx, p, driver.TapOptions{LongPress: held > 500*time.Millisecond})
	}
	if held < 80*time.Millisecond {
		held = 80 * time.Millisecond
	}
	if held > 2*time.Second {
		held = 2 * time.Second
	}
	return dev.Swipe(ctx, start.from, p, held)
}

// Key implements API.
func (g *Gateway) Key(deviceID string, k driver.Key) error {
	_, dev, err := g.personInput(deviceID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(g.ctx, 10*time.Second)
	defer cancel()
	return dev.Key(ctx, k)
}

// Text implements API.
func (g *Gateway) Text(deviceID string, text string) error {
	_, dev, err := g.personInput(deviceID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(g.ctx, 30*time.Second)
	defer cancel()
	return dev.Type(ctx, text, driver.TypeOptions{Append: true})
}

// PersonTouched records that the person touched the phone itself, which
// pauses the agent like input on the mirror does. Android drivers can see
// such touches (getevent on the touchscreen node), but driver.go has no way
// to report them yet; wire the driver's signal to this method.
func (g *Gateway) PersonTouched(deviceID string) error {
	d, err := g.device(deviceID)
	if err != nil {
		return err
	}
	g.takeOver(d, true)
	return nil
}

// personInput takes over for the person and returns the driver.
func (g *Gateway) personInput(deviceID string) (*device, driver.Device, error) {
	d, err := g.device(deviceID)
	if err != nil {
		return nil, nil, err
	}
	g.takeOver(d, true)
	g.mu.Lock()
	dev := d.dev
	g.mu.Unlock()
	if dev == nil {
		return nil, nil, driver.ErrOffline
	}
	return d, dev, nil
}

// The methods below are for the client but not part of API yet: settings,
// history files and agents.

// SetReadOnly turns read-only mode on or off: for every device when
// deviceID is empty (saved in the rules file), else for one device until
// the gateway restarts.
func (g *Gateway) SetReadOnly(deviceID string, on bool) error {
	err := g.policy.SetReadOnly(deviceID, on)
	g.emit(Event{Kind: EventDevices, DeviceID: deviceID})
	return err
}

// BlockedApps lists the apps agents may not use, for Settings › Safety: the
// apps the person blocked, and the apps on the connected phones that a rule
// blocks (banking, wallets, password managers). Phones that cannot list
// their apps are skipped.
func (g *Gateway) BlockedApps(ctx context.Context) []policy.BlockedApp {
	g.mu.Lock()
	var devs []driver.Device
	for _, d := range g.devices {
		if d.dev != nil {
			devs = append(devs, d.dev)
		}
	}
	g.mu.Unlock()
	var installed []screen.App
	for _, dev := range devs {
		apps, err := dev.Apps(ctx)
		if err != nil {
			continue
		}
		for _, a := range apps {
			installed = append(installed, screen.App{ID: a.ID, Name: a.Name})
		}
	}
	return g.policy.BlockedApps(installed)
}

// SetAppBlocked blocks or unblocks an app for agents and saves apps.yaml.
func (g *Gateway) SetAppBlocked(app screen.App, blocked bool) error {
	err := g.policy.SetAppBlocked(app, blocked)
	g.emit(Event{Kind: EventDevices})
	return err
}

// SetTaskApps replaces the app allow-list of the task running on a device.
func (g *Gateway) SetTaskApps(deviceID string, apps []string) error {
	d, err := g.device(deviceID)
	if err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if d.lease == nil {
		return errors.New("no task is running on this phone")
	}
	d.lease.apps = append([]string(nil), apps...)
	return nil
}

// MediaPath returns the file of a step's screenshot, by the media id in
// StepView.Before or After.
func (g *Gateway) MediaPath(ctx context.Context, id string) (string, error) {
	if g.store == nil {
		return "", history.ErrNoMedia
	}
	g.rec.flush()
	return g.store.MediaPath(ctx, id)
}

// WipeHistory deletes all history: tasks, steps, approvals and media.
func (g *Gateway) WipeHistory(ctx context.Context) error {
	if g.store == nil {
		g.mu.Lock()
		g.ended = nil
		g.mu.Unlock()
		return nil
	}
	g.rec.flush()
	if err := g.store.Wipe(ctx); err != nil {
		return err
	}
	g.mu.Lock()
	g.ended = nil
	for _, d := range g.devices {
		d.kept = nil
		if p := d.shot; p != nil {
			g.dropShotLocked(d, p)
		}
		if d.lease != nil {
			// Running tasks are written again so they keep showing up.
			g.recordTaskLocked(d.lease.task)
		}
	}
	g.mu.Unlock()
	g.emit(Event{Kind: EventTasks})
	return nil
}

// AgentView is an agent in Settings.
type AgentView struct {
	Name     string
	Approved bool
	Scope    Scope
}

// Agents lists the agents that connected.
func (g *Gateway) Agents(ctx context.Context) ([]AgentView, error) {
	seen := map[string]bool{}
	var out []AgentView
	if g.store != nil {
		recs, err := g.store.Agents(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range recs {
			seen[r.Name] = true
			out = append(out, AgentView{Name: r.Name, Approved: !r.ApprovedAt.IsZero(), Scope: ParseScope(r.Scope)})
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, a := range out {
		if st, ok := g.agents[a.Name]; ok {
			out[i].Approved, out[i].Scope = st.approved, st.scope
		}
	}
	for name, st := range g.agents {
		if !seen[name] {
			out = append(out, AgentView{Name: name, Approved: st.approved, Scope: st.scope})
		}
	}
	return out, nil
}

// SetAgentScope changes what an agent may do and approves it.
func (g *Gateway) SetAgentScope(name string, scope Scope) {
	g.setAgentApproved(AgentInfo{Name: name}, scope)
}
