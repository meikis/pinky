// Package fakeapi is a scripted gateway.API for demos, tests and screenshots
// of the desktop client. It plays the prototype's scenarios: an agent
// reordering lunch on a Pixel that asks to pay, an iPhone in an onboarding
// test, an offline Galaxy, a locked phone, and a person taking over. Its
// mirrors show ffmpeg test sources, never a real screen.
package fakeapi

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/gateway"
	"github.com/DIYgod/pinky/internal/screen"
)

// Scenario picks what the demo starts with.
type Scenario string

// Scenarios, after the prototype's artboards.
const (
	OnePhone    Scenario = "one"        // Pixel 9 reorders lunch, then asks to pay
	ThreePhones Scenario = "three"      // and an iPhone in an onboarding test, and an offline Galaxy
	Locked      Scenario = "locked"     // Pixel 9 is locked while the agent replies to Sam
	Takeover    Scenario = "takeover"   // the person took over Pixel 9 to change the note
	Handoff     Scenario = "handoff"    // the agent asks the person to sign in to Dinely
	NewAgent    Scenario = "agent"      // Codex connects for the first time and waits to be allowed
	Companion   Scenario = "companion"  // Pixel 9 lacks the companion app: its page offers to install it
	Background  Scenario = "background" // Pixel 9 works in the background: the mirror shows the agent's display
	NoPhones    Scenario = "empty"      // nothing connected: the client opens Add phone
	Checkup     Scenario = "checkup"    // a Redmi on HyperOS refuses taps and installs: its checkup says what to turn on
)

// Scenarios lists them in menu order.
var Scenarios = []Scenario{OnePhone, ThreePhones, Locked, Takeover, Handoff, NewAgent, Companion, Background, NoPhones, Checkup}

// Title names a scenario for menus.
func (s Scenario) Title() string {
	switch s {
	case OnePhone:
		return "One phone"
	case ThreePhones:
		return "Three phones"
	case Locked:
		return "Locked phone"
	case Takeover:
		return "You're in control"
	case Handoff:
		return "Handoff"
	case NewAgent:
		return "New agent"
	case Companion:
		return "Companion app missing"
	case Background:
		return "Background work"
	case NoPhones:
		return "No phones"
	case Checkup:
		return "Checkup of a Xiaomi phone"
	}
	return string(s)
}

// Options configure the fake.
type Options struct {
	Scenario Scenario
	// Speed scales the script's pace: 2 runs it twice as fast. 0 is 1.
	Speed float64
	// FFmpeg is the ffmpeg command for the test streams; "" finds it on PATH.
	FFmpeg string
	// ApprovalTimeout is how long an approval waits before it denies;
	// 0 is 60 s, the design's default.
	ApprovalTimeout time.Duration
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// PairDelay is how long a QR pairing waits before its fake phone scans
	// the code; 0 is a few seconds at Speed, and below 0 waits for
	// ScanPairing.
	PairDelay time.Duration
	// HoldSetup keeps a phone ConnectPhone plugs in at "USB debugging is
	// off" until FinishSetup, rather than for a few seconds at Speed.
	HoldSetup bool
}

// Input is one input the person sent through the mirror.
type Input struct {
	Device string
	Kind   string // "touch", "key", "text"
	Action driver.TouchAction
	Point  screen.Point
	Key    driver.Key
	Text   string
}

// API is the fake gateway. It is safe for concurrent use.
type API struct {
	opt Options

	mu        sync.Mutex
	gen       int // bumped by every scenario start; scripts of older ones stop
	scenario  Scenario
	devices   []*device
	tasks     []*task
	needs     []*need
	history   []gateway.TaskView
	subs      map[chan gateway.Event]struct{}
	inputs    []Input
	nextID    int
	shotCache map[string]image.Image

	agents     map[string]*agent
	readOnly   map[string]bool
	mediaDir   string            // where MediaPath writes screenshots
	mediaFiles map[string]string // media id → file

	pairings    []*pairing
	pairProg    driver.PairingProgress // what the open QR pairings report
	installs    []string
	updates     []string // companion apps UpdateCompanion updated
	noRecording bool
	keepDays    int
	unrecorded  map[string]bool // finished while recording was off
	recOpens    []time.Duration
	exports     []Export

	always   []gateway.Exemption // Always allow answers
	rules    []gateway.RuleView  // the rules file's rules
	rulesSet bool
	// fileProblems are rules files edited by hand that cannot be used.
	fileProblems []gateway.FileProblem
	removals     []Removal // RemoveDevice calls (remove.go)

	iosHost driver.HostCheck // what the computer lacks for iPhones (iphone.go)
	dlErr   error            // what DownloadDiskImage fails with (iphone.go)
}

type device struct {
	info       driver.Info
	width      int // the screen, in the pixels touches and targets use
	height     int
	codec      driver.Codec
	source     string // ffmpeg lavfi test source
	locked     bool
	inControl  bool
	statusLine string                // when an agent works on it
	task       *task                 // the task running on it, if any
	companion  driver.CompanionState // Android only; "" is on
	background bool                  // agents work on a background display
	checkup    *driver.Checkup       // what its own checkup finds; nil for the default
	installErr error                 // what installing the companion app fails with
	// signedElsewhere: the installed companion is signed with the public
	// key of builds until 2026-11-08, so updating replaces it.
	signedElsewhere bool
	way             string // iPhone: the connection the person chose
}

type task struct {
	view    gateway.TaskView
	short   string // "Reorder lunch", for the sidebar
	dev     *device
	wake    chan struct{}
	ended   time.Time // when it finished; it stays in Tasks for a while
	removed bool
}

type need struct {
	view     gateway.NeedView
	decision chan decision
}

type decision struct {
	d    gateway.Decision
	note string
}

// New returns a fake gateway playing the scenario.
func New(opt Options) *API {
	if opt.Speed <= 0 {
		opt.Speed = 1
	}
	if opt.ApprovalTimeout <= 0 {
		opt.ApprovalTimeout = 60 * time.Second
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Scenario == "" {
		opt.Scenario = OnePhone
	}
	a := &API{opt: opt, subs: map[chan gateway.Event]struct{}{}, shotCache: map[string]image.Image{},
		agents: defaultAgents(), mediaFiles: map[string]string{}, unrecorded: map[string]bool{}, keepDays: 30}
	a.history = seedHistory(opt.Now())
	a.Play(opt.Scenario)
	return a
}

// Scenario returns the scenario playing.
func (a *API) Scenario() Scenario {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.scenario
}

// Play starts a scenario from the beginning, ending the running one.
func (a *API) Play(s Scenario) {
	a.mu.Lock()
	a.gen++
	gen := a.gen
	a.scenario = s
	for _, t := range a.tasks {
		t.signal()
	}
	for _, n := range a.needs {
		close(n.decision)
	}
	a.needs = nil
	a.tasks = nil
	a.devices = nil
	pixel := &device{
		info: driver.Info{ID: "pixel-9", Platform: driver.Android, Name: "Pixel 9", Model: "Pixel 9",
			OSVersion: "16", Connection: "usb", Backend: "scrcpy", State: driver.StateOnline},
		width: 1080, height: 2400, codec: driver.H264, source: "testsrc2",
	}
	iphone := &device{
		info: driver.Info{ID: "iphone-17", Platform: driver.IOS, Name: "iPhone 17", Model: "iPhone 17",
			OSVersion: "27.1", Connection: "wifi", Backend: "coredevice", State: driver.StateOnline},
		width: 1206, height: 2622, codec: driver.HEVC, source: "testsrc",
	}
	galaxy := &device{
		info: driver.Info{ID: "galaxy-s25", Platform: driver.Android, Name: "Galaxy S25", Model: "Galaxy S25",
			OSVersion: "15", Connection: "usb", Backend: "scrcpy", State: driver.StateOffline, Detail: "Last seen 2 days ago"},
		width: 1080, height: 2340, codec: driver.H264, source: "smptehdbars",
	}
	var scripts []func()
	switch s {
	case OnePhone:
		a.devices = []*device{pixel}
		scripts = append(scripts, a.lunch(gen, pixel, false))
	case ThreePhones:
		a.devices = []*device{pixel, iphone, galaxy}
		scripts = append(scripts, a.lunch(gen, pixel, false), a.onboarding(gen, iphone))
	case Locked:
		a.devices = []*device{pixel}
		pixel.locked = true
		scripts = append(scripts, a.replyToSam(gen, pixel))
	case Takeover:
		a.devices = []*device{pixel}
		scripts = append(scripts, a.lunch(gen, pixel, true))
	case Handoff:
		a.devices = []*device{pixel}
		scripts = append(scripts, a.signIn(gen, pixel))
	case NewAgent:
		a.devices = []*device{pixel}
		scripts = append(scripts, a.newAgent(gen, pixel))
	case Companion:
		pixel.companion = driver.CompanionMissing
		a.devices = []*device{pixel}
	case Background:
		pixel.background = true
		a.devices = []*device{pixel}
		scripts = append(scripts, a.lunch(gen, pixel, false))
	case NoPhones:
	case Checkup:
		redmi := &device{
			info: driver.Info{ID: "redmi-k80", Platform: driver.Android, Name: "Redmi K80", Model: "Redmi K80",
				OSVersion: "15", Connection: "usb", Backend: "scrcpy", State: driver.StateOnline},
			width: 1440, height: 3200, codec: driver.H264, source: "testsrc2", companion: driver.CompanionMissing,
			installErr: errRestricted,
		}
		cu := XiaomiCheckup()
		redmi.checkup = &cu
		a.devices = []*device{redmi}
	}
	a.mu.Unlock()
	for _, run := range scripts {
		go run()
	}
	a.emit(gateway.Event{Kind: gateway.EventDevices})
	a.emit(gateway.Event{Kind: gateway.EventTasks})
	a.emit(gateway.Event{Kind: gateway.EventNeeds})
}

// ConnectPhone plugs in a new Android phone, as the Add phone flow waits
// for: it shows up waiting for the person to allow USB debugging on it,
// then comes online a few seconds later at Speed, or at FinishSetup with
// Options.HoldSetup.
func (a *API) ConnectPhone() { a.ConnectPhoneAs("Pixel 8a", nil) }

// ConnectPhoneAs is ConnectPhone with the phone's name and, when cu is
// set, what its checkup finds once it is online. It returns its id.
func (a *API) ConnectPhoneAs(name string, cu *driver.Checkup) string {
	a.mu.Lock()
	gen := a.gen
	a.nextID++
	id := fmt.Sprintf("%s-%d", strings.ToLower(strings.ReplaceAll(name, " ", "-")), a.nextID)
	d := &device{
		info: driver.Info{ID: id, Platform: driver.Android, Name: name, Model: name, OSVersion: "16",
			Connection: "usb", Backend: "scrcpy", State: driver.StateUnauthorized,
			Detail: "Unlock the phone and allow USB debugging for this computer."},
		width: 1080, height: 2400, codec: driver.H264, source: "rgbtestsrc", checkup: cu,
	}
	a.devices = append(a.devices, d)
	a.mu.Unlock()
	a.emit(gateway.Event{Kind: gateway.EventDevices, DeviceID: id})
	if a.opt.HoldSetup {
		return id
	}
	go func() {
		if a.sleepGen(gen, 4*time.Second) {
			a.finishSetup(d)
		}
	}()
	return id
}

// FinishSetup brings the phones ConnectPhone plugged in online now, as
// when the person turns USB debugging on.
func (a *API) FinishSetup() {
	a.mu.Lock()
	var waiting []*device
	for _, d := range a.devices {
		if d.info.State == driver.StateNeedsSetup || d.info.State == driver.StateUnauthorized {
			waiting = append(waiting, d)
		}
	}
	a.mu.Unlock()
	for _, d := range waiting {
		a.finishSetup(d)
	}
}

func (a *API) finishSetup(d *device) {
	a.mu.Lock()
	if d.info.State != driver.StateNeedsSetup && d.info.State != driver.StateUnauthorized {
		a.mu.Unlock()
		return
	}
	d.info.State, d.info.Detail = driver.StateOnline, ""
	id := d.info.ID
	a.mu.Unlock()
	a.emit(gateway.Event{Kind: gateway.EventDevices, DeviceID: id})
}

// Inputs returns the person's input received so far.
func (a *API) Inputs() []Input {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Input(nil), a.inputs...)
}

// --- gateway.API ---

// Devices implements gateway.API.
func (a *API) Devices() []gateway.DeviceView {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]gateway.DeviceView, 0, len(a.devices))
	for _, d := range a.devices {
		out = append(out, a.deviceView(d))
	}
	return out
}

func (a *API) deviceView(d *device) gateway.DeviceView {
	v := gateway.DeviceView{Info: d.info, Features: a.features(d)}
	running := d.task != nil && !isFinished(d.task.view.State)
	if running {
		v.TaskID = d.task.view.ID
	}
	switch {
	case d.info.State == driver.StateOffline:
		v.Status, v.StatusLine = gateway.DeviceOffline, "Offline"
	case d.info.State != driver.StateOnline:
		v.Status, v.StatusLine = gateway.DeviceNeedsYou, "Needs setup"
	case running && (a.hasNeed(d, gateway.NeedApproval) || a.hasNeed(d, gateway.NeedHandoff)):
		v.Status, v.StatusLine = gateway.DeviceNeedsYou, d.statusLine
	case d.locked && running:
		v.Status, v.StatusLine = gateway.DeviceLocked, "Locked"
	case d.inControl:
		v.Status, v.StatusLine = gateway.DeviceInControl, "You're in control"
	case running:
		v.Status, v.StatusLine = gateway.DeviceWorking, d.statusLine
	case d.locked:
		v.Status, v.StatusLine = gateway.DeviceLocked, "Locked"
	default:
		v.Status, v.StatusLine = gateway.DeviceIdle, "Idle"
	}
	return v
}

func (a *API) hasNeed(d *device, kind gateway.NeedKind) bool {
	for _, n := range a.needs {
		if n.view.DeviceID == d.info.ID && n.view.Kind == kind {
			return true
		}
	}
	return false
}

// Tasks implements gateway.API: running tasks, and finished ones for two
// minutes after they end, so their outcome shows on the task card.
func (a *API) Tasks() []gateway.TaskView {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.opt.Now()
	var out []gateway.TaskView
	for _, t := range a.tasks {
		if t.removed || (!t.ended.IsZero() && now.Sub(t.ended) > 2*time.Minute) {
			continue
		}
		out = append(out, cloneTask(t.view))
	}
	return out
}

// Needs implements gateway.API.
func (a *API) Needs() []gateway.NeedView {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]gateway.NeedView, 0, len(a.needs))
	for _, n := range a.needs {
		out = append(out, n.view)
	}
	return out
}

// History implements gateway.API: finished tasks, newest first.
func (a *API) History(_ context.Context, q gateway.HistoryQuery) ([]gateway.TaskView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	search := strings.ToLower(strings.TrimSpace(q.Search))
	var out []gateway.TaskView
	for _, t := range a.history {
		if !q.Before.IsZero() && !t.Started.Before(q.Before) {
			continue
		}
		if search != "" && !matches(t, search) {
			continue
		}
		out = append(out, cloneTask(t))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func matches(t gateway.TaskView, q string) bool {
	if strings.Contains(strings.ToLower(t.Title+" "+t.DeviceName+" "+t.Agent), q) {
		return true
	}
	for _, s := range t.Steps {
		if strings.Contains(strings.ToLower(s.Text), q) {
			return true
		}
	}
	return false
}

// Subscribe implements gateway.API.
func (a *API) Subscribe(ctx context.Context) <-chan gateway.Event {
	ch := make(chan gateway.Event, 256)
	a.mu.Lock()
	a.subs[ch] = struct{}{}
	a.mu.Unlock()
	go func() {
		<-ctx.Done()
		a.mu.Lock()
		delete(a.subs, ch)
		a.mu.Unlock()
		close(ch)
	}()
	return ch
}

func (a *API) emit(ev gateway.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for ch := range a.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Decide implements gateway.API, for approvals and handoffs. Take over
// pauses the task for the person, as TakeOver does.
func (a *API) Decide(needID string, d gateway.Decision, note string) error {
	a.mu.Lock()
	var n *need
	idx := -1
	for i, x := range a.needs {
		if x.view.ID == needID {
			n, idx = x, i
			break
		}
	}
	if n == nil {
		a.mu.Unlock()
		return fmt.Errorf("fakeapi: no approval %q", needID)
	}
	if n.view.Kind != gateway.NeedApproval && n.view.Kind != gateway.NeedHandoff {
		a.mu.Unlock()
		return fmt.Errorf("fakeapi: %q is not an approval", needID)
	}
	if dev := a.device(n.view.DeviceID); d == gateway.TakeOver && dev != nil && dev.task != nil && !isFinished(dev.task.view.State) {
		a.takeOverLocked(dev) // answers this need and the phone's others
		a.mu.Unlock()
		a.emitAll(n.view.DeviceID)
		return nil
	}
	a.needs = append(a.needs[:idx], a.needs[idx+1:]...)
	if d == gateway.AllowAlways && n.view.AlwaysAllow {
		a.keepAlwaysLocked(gateway.Exemption{Rule: n.view.Rule, AppName: n.view.App})
	}
	a.mu.Unlock()
	n.decision <- decision{d: d, note: note}
	a.emit(gateway.Event{Kind: gateway.EventNeeds, NeedID: needID, DeviceID: n.view.DeviceID})
	a.emit(gateway.Event{Kind: gateway.EventDevices, DeviceID: n.view.DeviceID})
	return nil
}

// Unlock implements gateway.API.
func (a *API) Unlock(deviceID string) error {
	a.mu.Lock()
	d := a.device(deviceID)
	if d == nil {
		a.mu.Unlock()
		return fmt.Errorf("fakeapi: no device %q: %w", deviceID, driver.ErrNotFound)
	}
	var n *need
	for i, x := range a.needs {
		if x.view.DeviceID == deviceID && x.view.Kind == gateway.NeedUnlock {
			n = x
			a.needs = append(a.needs[:i], a.needs[i+1:]...)
			break
		}
	}
	d.locked = false
	a.mu.Unlock()
	if n != nil {
		n.decision <- decision{d: gateway.AllowOnce}
	}
	a.emit(gateway.Event{Kind: gateway.EventNeeds, DeviceID: deviceID})
	a.emit(gateway.Event{Kind: gateway.EventDevices, DeviceID: deviceID})
	return nil
}

// TakeOver implements gateway.API.
func (a *API) TakeOver(deviceID string) error {
	a.mu.Lock()
	d := a.device(deviceID)
	if d == nil {
		a.mu.Unlock()
		return fmt.Errorf("fakeapi: no device %q: %w", deviceID, driver.ErrNotFound)
	}
	changed := a.takeOverLocked(d)
	a.mu.Unlock()
	if changed {
		a.emitAll(deviceID)
	}
	return nil
}

// takeOverLocked pauses the device's task for the person. It withdraws
// the device's open approvals, which the agent asks again for later.
func (a *API) takeOverLocked(d *device) bool {
	if d.inControl {
		return false
	}
	d.inControl = true
	t := d.task
	if t == nil || isFinished(t.view.State) {
		return true
	}
	handoff := false
	for i := 0; i < len(a.needs); i++ {
		if n := a.needs[i]; n.view.DeviceID == d.info.ID && (n.view.Kind == gateway.NeedApproval || n.view.Kind == gateway.NeedHandoff) {
			a.needs = append(a.needs[:i], a.needs[i+1:]...)
			i--
			handoff = handoff || n.view.Kind == gateway.NeedHandoff
			n.decision <- decision{d: gateway.TakeOver}
		}
	}
	// The agent's pending step is withdrawn with its approval; a handoff's
	// stays, and settles when the person hands back.
	if n := len(t.view.Steps); n > 0 && t.view.Steps[n-1].Status == "approval_pending" && !handoff {
		t.view.Steps = t.view.Steps[:n-1]
	}
	t.view.State = gateway.TaskPaused
	t.addStep(a.opt.Now(), gateway.StepView{Text: "You took over", Status: "user"})
	t.signal()
	return true
}

// HandBack implements gateway.API.
func (a *API) HandBack(deviceID, note string) error {
	a.mu.Lock()
	d := a.device(deviceID)
	if d == nil {
		a.mu.Unlock()
		return fmt.Errorf("fakeapi: no device %q: %w", deviceID, driver.ErrNotFound)
	}
	if !d.inControl {
		a.mu.Unlock()
		return nil
	}
	d.inControl = false
	if t := d.task; t != nil && !isFinished(t.view.State) {
		t.view.State = gateway.TaskWorking
		if n := len(t.view.Steps); n > 0 && t.view.Steps[n-1].Text == "You are changing the note" {
			t.view.Steps[n-1].Text = "You changed the note"
		}
		text := "You handed back control"
		if note != "" {
			text += "\nYour note: “" + note + "”"
		}
		for i := range t.view.Steps {
			if st := &t.view.Steps[i]; st.Status == "approval_pending" && strings.HasPrefix(st.Text, "Asked you to take over: ") {
				st.Status = "settled"
			}
		}
		t.finishCurrent()
		t.addStep(a.opt.Now(), gateway.StepView{Text: text, Status: "user"})
		t.signal()
	}
	a.mu.Unlock()
	a.emitAll(deviceID)
	return nil
}

// Stop implements gateway.API.
func (a *API) Stop(deviceID string) error {
	a.mu.Lock()
	d := a.device(deviceID)
	if d == nil {
		a.mu.Unlock()
		return fmt.Errorf("fakeapi: no device %q: %w", deviceID, driver.ErrNotFound)
	}
	a.stopLocked(d)
	a.mu.Unlock()
	a.emitAll(deviceID)
	return nil
}

func (a *API) stopLocked(d *device) {
	d.inControl = false
	for i := 0; i < len(a.needs); i++ {
		if n := a.needs[i]; n.view.DeviceID == d.info.ID {
			a.needs = append(a.needs[:i], a.needs[i+1:]...)
			i--
			close(n.decision)
		}
	}
	if t := d.task; t != nil && !isFinished(t.view.State) {
		t.finishCurrent()
		t.addStep(a.opt.Now(), gateway.StepView{Text: "You stopped the task", Status: "user"})
		a.finishLocked(t, gateway.TaskStopped)
	}
}

// StopAll implements gateway.API.
func (a *API) StopAll() error {
	a.mu.Lock()
	for _, d := range a.devices {
		a.stopLocked(d)
	}
	a.mu.Unlock()
	a.emitAll("")
	return nil
}

// Stream implements gateway.API with an ffmpeg test source in the
// device's codec and screen size, scaled down to MaxSize.
func (a *API) Stream(ctx context.Context, deviceID string, opt driver.StreamOptions) (driver.Stream, error) {
	a.mu.Lock()
	d := a.device(deviceID)
	if d == nil {
		a.mu.Unlock()
		return nil, fmt.Errorf("fakeapi: no device %q: %w", deviceID, driver.ErrNotFound)
	}
	if d.info.State != driver.StateOnline {
		a.mu.Unlock()
		return nil, fmt.Errorf("fakeapi: %s: %w", d.info.Name, driver.ErrOffline)
	}
	w, h, codec, src := d.width, d.height, d.codec, d.source
	a.mu.Unlock()
	if opt.MaxSize > 0 && max(w, h) > opt.MaxSize {
		s := float64(opt.MaxSize) / float64(max(w, h))
		w, h = int(float64(w)*s)&^1, int(float64(h)*s)&^1
	}
	fps := 60
	if opt.MaxFPS > 0 && opt.MaxFPS < fps {
		fps = opt.MaxFPS
	}
	return startStream(ctx, a.opt.FFmpeg, codec, src, w, h, fps)
}

// Touch implements gateway.API. Input from the person pauses the agent.
func (a *API) Touch(deviceID string, act driver.TouchAction, p screen.Point) error {
	return a.input(Input{Device: deviceID, Kind: "touch", Action: act, Point: p})
}

// Key implements gateway.API.
func (a *API) Key(deviceID string, k driver.Key) error {
	return a.input(Input{Device: deviceID, Kind: "key", Key: k})
}

// Text implements gateway.API.
func (a *API) Text(deviceID string, text string) error {
	return a.input(Input{Device: deviceID, Kind: "text", Text: text})
}

func (a *API) input(in Input) error {
	a.mu.Lock()
	d := a.device(in.Device)
	if d == nil {
		a.mu.Unlock()
		return fmt.Errorf("fakeapi: no device %q: %w", in.Device, driver.ErrNotFound)
	}
	if d.info.State != driver.StateOnline {
		a.mu.Unlock()
		return fmt.Errorf("fakeapi: %s: %w", d.info.Name, driver.ErrOffline)
	}
	a.inputs = append(a.inputs, in)
	changed := false
	if t := d.task; t != nil && !isFinished(t.view.State) {
		changed = a.takeOverLocked(d)
	}
	a.mu.Unlock()
	if changed {
		a.emitAll(in.Device)
	}
	return nil
}

func (a *API) emitAll(deviceID string) {
	a.emit(gateway.Event{Kind: gateway.EventDevices, DeviceID: deviceID})
	a.emit(gateway.Event{Kind: gateway.EventTasks, DeviceID: deviceID})
	a.emit(gateway.Event{Kind: gateway.EventNeeds, DeviceID: deviceID})
}

func (a *API) device(id string) *device {
	for _, d := range a.devices {
		if d.info.ID == id {
			return d
		}
	}
	return nil
}

// finishLocked ends a task and files it in history.
func (a *API) finishLocked(t *task, state gateway.TaskState) {
	now := a.opt.Now()
	t.view.State = state
	t.view.Ended = now
	t.ended = now
	t.dev.inControl = false
	t.signal()
	if a.noRecording {
		a.unrecorded[t.view.ID] = true
	}
	a.history = append(a.history, cloneTask(t.view))
}

// --- helpers ---

func isFinished(s gateway.TaskState) bool {
	return s == gateway.TaskDone || s == gateway.TaskStopped || s == gateway.TaskFailed
}

func (t *task) signal() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

func (t *task) addStep(now time.Time, s gateway.StepView) {
	s.Index = len(t.view.Steps)
	if s.At.IsZero() {
		s.At = now
	}
	t.view.Steps = append(t.view.Steps, s)
}

// finishCurrent marks a step still in progress as settled.
func (t *task) finishCurrent() {
	if n := len(t.view.Steps); n > 0 && t.view.Steps[n-1].Status == "" {
		t.view.Steps[n-1].Status = "settled"
	}
}

func cloneTask(t gateway.TaskView) gateway.TaskView {
	t.Steps = append([]gateway.StepView(nil), t.Steps...)
	return t
}

var errStopped = errors.New("fakeapi: scenario stopped")

var _ gateway.API = (*API)(nil)
