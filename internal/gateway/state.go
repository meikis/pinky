package gateway

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/screen"
)

// device is one phone the gateway knows. Fields below act are guarded by
// Gateway.mu.
type device struct {
	id       string
	provider driver.Provider
	refs     *screen.Refs
	nameV    atomic.Value // string; read without the lock

	// act serializes agent actions: read the screen, decide, act, settle and
	// read again happen as one unit per device.
	act sync.Mutex

	info         driver.Info
	dev          driver.Device // nil while offline or not opened yet
	opening      bool
	online       bool
	removed      bool // the person removed it (remove.go); a new device takes its place if it comes back
	offlineSince time.Time
	offlineWhy   string
	lease        *lease
	// person is true while the person is in control (took over). It belongs
	// to the device, not the lease: only HandBack clears it, so an agent
	// cannot get control back by starting another task.
	person bool
	// personInput is true once the person used the phone during a takeover.
	personInput bool
	// working is what the driver was last told (driver.WorkWatcher): an
	// agent works on the phone. workSent: dev was told at all.
	working, workSent bool
	locked            bool
	// inflight cancels the driver call of the action in progress, so a
	// takeover interrupts it.
	inflight context.CancelFunc
	// events holds toasts, notifications and dialogs since the agent's last
	// result, already filtered by the policy.
	events []screen.Event
	last   *screen.Screen // the last raw observation
	// ocr is set once a read with OCR returned OCR lines: the phone's
	// driver reads text from the pixels (iPhone), so a tap on text that
	// only OCR sees is checked against it (ocrForVerdict).
	ocr bool
	// inputs counts input sent to the phone, by the agent or the person. A
	// history screenshot is dropped when input came while it was taken.
	inputs uint64
	// shot is the history screenshot being taken, or waiting for a read to
	// confirm it; kept is the last one kept, which steps reuse while the
	// screen matches (see shots.go).
	shot, kept *bgShot
	// video records the running task's screen (video.go).
	video *taskVideo
	// wokeAt is when the gateway last woke the screen, or found it on.
	wokeAt time.Time
	// lockReadAt is when an idle phone's lock state was last read.
	lockReadAt time.Time
	// personAt is when the person last used the phone's own screen while
	// an agent worked on a background display (which does not pause it).
	personAt time.Time
	touch    *touchState
	// wake is closed and replaced on every state change, to wake waiting calls.
	wake chan struct{}
}

func newDevice(p driver.Provider, info driver.Info) *device {
	d := &device{id: info.ID, provider: p, info: info, refs: screen.NewRefs(), wake: make(chan struct{})}
	d.nameV.Store(info.Name)
	return d
}

// name returns the device's display name. It needs no lock.
func (d *device) name() string {
	if v, _ := d.nameV.Load().(string); v != "" {
		return v
	}
	return d.id
}

type touchState struct {
	from screen.Point
	at   time.Time
}

// ScopeMode is what an agent may do.
type ScopeMode string

// Scope modes.
const (
	ScopeEverything ScopeMode = "everything"
	ScopeReadOnly   ScopeMode = "read_only"
	ScopeWatchOnly  ScopeMode = "watch_only"
	ScopeApps       ScopeMode = "apps" // only the listed apps
)

// Scope is the person's grant to an agent at its first connection.
type Scope struct {
	Mode ScopeMode
	Apps []string // for ScopeApps: ids or names
}

// String encodes the scope for storage: "everything", "apps:a,b".
func (s Scope) String() string {
	if s.Mode == ScopeApps {
		return "apps:" + strings.Join(s.Apps, ",")
	}
	return string(s.Mode)
}

// ParseScope decodes a stored scope. Unknown values become read-only, the
// safe side.
func ParseScope(v string) Scope {
	switch {
	case v == "" || v == string(ScopeEverything):
		return Scope{Mode: ScopeEverything}
	case v == string(ScopeReadOnly):
		return Scope{Mode: ScopeReadOnly}
	case v == string(ScopeWatchOnly):
		return Scope{Mode: ScopeWatchOnly}
	case strings.HasPrefix(v, "apps:"):
		var apps []string
		for _, a := range strings.Split(strings.TrimPrefix(v, "apps:"), ",") {
			if a = strings.TrimSpace(a); a != "" {
				apps = append(apps, a)
			}
		}
		return Scope{Mode: ScopeApps, Apps: apps}
	}
	return Scope{Mode: ScopeReadOnly}
}

type agentState struct {
	name     string
	approved bool
	scope    Scope
}

// lease ties a device to a session for one task. Guarded by Gateway.mu,
// except ctx and cancel, which are set once.
type lease struct {
	id       string
	session  *Session
	dev      *device
	task     *task
	created  time.Time
	lastUsed time.Time
	inflight int
	ended    bool
	endedBy  string // an Outcome
	// baseline is the last screen the agent received, for diffs.
	baseline *screen.Screen
	// apps is the task's app allow-list, ids and names.
	apps []string
	// grants holds the rules the person allowed for the rest of the task,
	// per app: grantKey(rule, app).
	grants map[string]bool
	// unblocked lists the blocked apps the person allowed for the rest of
	// the task, and kept the apps whose opening they refused in it
	// (unblock.go).
	unblocked, kept []screen.App
	// readOnly is set when the agent asked for read-only at phone_connect:
	// the task runs read-only whatever the agent's scope.
	readOnly bool
	// tookOverSeen is set once the agent got user_took_over for the current
	// takeover; later calls get paused.
	tookOverSeen bool
	// handedBack holds the person's note when they hand back before the
	// agent calls phone_handoff.
	handedBack   bool
	handBackNote string
	// unlockHandoffs are handoffs opened while the phone was locked that ask
	// only for unlocking (unlockOnly); Unlock from the computer ends them.
	unlockHandoffs []string
	// after is the read that ended the last action, if it settled; the next
	// step of a phone_batch reuses it while it is fresh.
	after   *screen.Screen
	afterAt time.Time
	// bg is the background display the agent works on (background.go),
	// nil on the phone's own screen. bgOff is set once the task left it,
	// and bgNote says why, for the agent's next result.
	bg     driver.Device
	bgOff  bool
	bgNote string
	// bgWanted is whether the task uses a background display, decided when
	// it starts: turning the setting on or off applies from the next task.
	bgWanted bool
	// filesGranted is set when the person allowed copying files to the
	// phone for the rest of the task.
	filesGranted bool
	// recorded is set once the task's recording started (video.go).
	recorded bool

	ctx    context.Context
	cancel context.CancelFunc
}

// task is an agent task on one device. Guarded by Gateway.mu.
type task struct {
	id         string
	sessionID  string
	title      string
	agent      string
	deviceID   string
	deviceName string
	state      TaskState
	started    time.Time
	ended      time.Time
	outcome    string
	summary    string
	steps      []StepView
	stepCount  int
	approvals  int
	next       int  // the next step index
	readOnly   bool // the agent asked for read-only (TaskView.ReadOnly)
}

// maxTaskSteps bounds the steps a running task keeps in memory; history
// keeps them all.
const maxTaskSteps = 500

// stepRef points at a step held by an approval, so the repeated call can
// finish the same step.
type stepRef struct {
	task  *task
	index int
	tool  string
	args  string
	start time.Time
}

// Outcomes record how a task ended.
const (
	OutcomeDone         = "done"
	OutcomeFailed       = "failed"
	OutcomeStopped      = "stopped"      // the person pressed Stop
	OutcomeExpired      = "expired"      // idle too long
	OutcomeDisconnected = "disconnected" // the agent's session closed
	OutcomeOffline      = "offline"      // the phone did not come back in time
	OutcomeReplaced     = "replaced"     // the agent started another task on the device
)

// stateForOutcome maps how a task ended to its card state. An agent that
// leaves or idles without phone_release has not failed, so those count as
// done; history keeps the exact outcome.
func stateForOutcome(o string) TaskState {
	switch o {
	case OutcomeDone, OutcomeReplaced, OutcomeDisconnected, OutcomeExpired, "partial":
		return TaskDone
	case OutcomeStopped:
		return TaskStopped
	}
	return TaskFailed
}

func equalFold(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

// grantKey names a task grant: a rule in an app.
func grantKey(rule string, app screen.App) string {
	return rule + "\x00" + app.ID + "\x00" + strings.ToLower(app.Name)
}

// mergeCtx returns a context that ends when either parent ends.
func mergeCtx(a, b context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(a)
	stop := context.AfterFunc(b, cancel)
	return ctx, func() { stop(); cancel() }
}
