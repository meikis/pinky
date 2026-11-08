package gateway

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/DIYgod/pinky/internal/approval"
	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/history"
	"github.com/DIYgod/pinky/internal/screen"
)

// This file is the gateway's agent-facing side. The MCP server opens one
// Session per MCP session and calls these methods; nothing here can approve,
// stop or take over, which only the client API can do.

// AgentInfo identifies the agent behind a session.
type AgentInfo struct {
	Name   string // display name, such as "Claude Code"
	Client string // the MCP client's own name and version
}

// Status is the outcome of a call, from DESIGN.md's Results table.
type Status string

// Statuses.
const (
	StatusSettled   Status = "settled"
	StatusUncertain Status = "uncertain"
	StatusBlocked   Status = "blocked_by_policy"
	StatusDenied    Status = "approval_denied"
	StatusPending   Status = "approval_pending"
	StatusStale     Status = "stale"
	StatusTookOver  Status = "user_took_over"
	StatusPaused    Status = "paused"
	StatusStopped   Status = "stopped"
	StatusLocked    Status = "device_locked"
)

// ErrorCode is an error a call returns instead of a status.
type ErrorCode string

// Error codes. The first five are DESIGN.md's; invalid_argument covers bad
// arguments and misused approval ids, internal covers gateway failures.
const (
	ErrDeviceOffline   ErrorCode = "device_offline"
	ErrLeaseHeld       ErrorCode = "lease_held"
	ErrScopeDenied     ErrorCode = "scope_denied"
	ErrNotFound        ErrorCode = "not_found"
	ErrUnsupported     ErrorCode = "unsupported"
	ErrInvalidArgument ErrorCode = "invalid_argument"
	ErrInternal        ErrorCode = "internal"
)

// Result is what an agent call returns. Either Error or Status is set.
type Result struct {
	Status     Status
	Error      ErrorCode
	Message    string // one or two plain sentences for the agent
	Device     string
	DeviceName string
	// Screen is the screen after the call as the agent may see it: blocked
	// apps are hidden and events are filtered.
	Screen *screen.Screen
	// Diff holds the lines that changed since the agent's last screen; when
	// Full is set, send Screen whole instead.
	Diff       []string
	Full       bool
	Settle     string // "timeout" when settling hit its limit
	ApprovalID string
	Rule       string
	Note       string
	Decision   string // phone_request_approval: "allow" or "deny"
	Holder     string // lease_held: who holds the device
	Matched    *bool  // phone_wait_for
	Apps       []driver.App
	Screenshot image.Image
	Steps      []*Result // phone_batch, one per step that ran
	Task       string    // phone_connect: the task id
	Folder     string    // phone_files: the folder on the computer
	Files      []FileView
}

// IsError reports whether the result is an error rather than a status.
func (r *Result) IsError() bool { return r.Error != "" }

func errResult(code ErrorCode, format string, args ...any) *Result {
	return &Result{Error: code, Message: fmt.Sprintf(format, args...)}
}

func statusResult(st Status, format string, args ...any) *Result {
	return &Result{Status: st, Message: fmt.Sprintf(format, args...)}
}

// Progress reports progress of a blocking call: done and total in seconds.
type Progress func(done, total float64, message string)

// Call holds the arguments every action tool takes.
type Call struct {
	Device     string
	Intent     string
	ApprovalID string
	Progress   Progress

	// reuse lets a batch step start from the previous step's read.
	reuse bool
}

// reuseAge is how old the previous step's read may be for the next batch
// step to start from it instead of reading the screen again.
const reuseAge = time.Second

// ConnectRequest is phone_connect.
type ConnectRequest struct {
	Call
	Task string
	// Apps is the task's app allow-list, ids or names. Launching any other
	// app is refused and typing into one asks the person.
	Apps []string
	// ReadOnly runs the task read-only, whatever the agent's scope: the
	// agent can ask for less than the person gave it, never more.
	ReadOnly bool
}

// ReleaseRequest is phone_release.
type ReleaseRequest struct {
	Call
	Outcome string // done, failed, partial or cancelled
	Summary string
}

// ObserveRequest is phone_observe.
type ObserveRequest struct {
	Call
	WaitMS     int
	Screenshot bool
}

// WaitForRequest is phone_wait_for.
type WaitForRequest struct {
	Call
	Text      string
	Gone      bool
	TimeoutMS int
}

// AppsRequest is phone_apps.
type AppsRequest struct {
	Call
	Query string
}

// ApprovalRequest is phone_request_approval.
type ApprovalRequest struct {
	Call
	Summary string
	Details string
}

// HandoffRequest is phone_handoff.
type HandoffRequest struct {
	Call
	Reason string
}

// BatchRequest is phone_batch.
type BatchRequest struct {
	Call
	Steps []Action
}

// Limits from DESIGN.md's tool table.
const (
	MaxTextLen     = 16384
	MaxObserveWait = 30 * time.Second
	MaxWaitFor     = 45 * time.Second
	MaxBatchSteps  = 50
)

// waitForOCREvery is how often phone_wait_for also reads the screen with
// OCR, which is slow on phones that need a screenshot for it.
const waitForOCREvery = time.Second

// Session is one agent connection. It may hold several devices.
type Session struct {
	g       *Gateway
	id      string
	agent   AgentInfo
	started time.Time

	// Guarded by Gateway.mu.
	leases  map[string]*lease // by device id
	barred  map[string]bool   // devices the person stopped this session on
	used    []string          // every device it leased
	closed  bool
	stopped bool // the person pressed Stop all
}

// OpenSession starts a session for an agent connection.
func (g *Gateway) OpenSession(a AgentInfo) *Session {
	if strings.TrimSpace(a.Name) == "" {
		a.Name = "Agent"
	}
	s := &Session{g: g, id: history.NewID("s_"), agent: a, started: time.Now(),
		leases: map[string]*lease{}, barred: map[string]bool{}}
	g.mu.Lock()
	g.sessions[s.id] = s
	g.mu.Unlock()
	g.recordSession(s)
	return s
}

// ID returns the session id.
func (s *Session) ID() string { return s.id }

// Agent returns who the session belongs to.
func (s *Session) Agent() AgentInfo { return s.agent }

// Close ends the session: its leases end and its open approvals are
// withdrawn without acting.
func (s *Session) Close() { s.end(OutcomeDisconnected) }

func (s *Session) end(outcome string) {
	g := s.g
	g.mu.Lock()
	if s.closed {
		g.mu.Unlock()
		return
	}
	s.closed = true
	delete(g.sessions, s.id)
	for _, l := range s.leases {
		g.endLeaseLocked(l, outcome, "")
	}
	g.mu.Unlock()
	gone := g.approvals.WithdrawWhere(func(i approval.Info) bool { return i.Binding.Session == s.id }, "the agent disconnected")
	g.closePendingSteps(gone, "withdrawn")
	g.recordSession(s)
}

// acquire finds the lease a call acts on and marks it busy.
func (s *Session) acquire(ref string) (*lease, *Result) {
	g := s.g
	g.mu.Lock()
	defer g.mu.Unlock()
	if s.closed {
		return nil, errResult(ErrInternal, "This session is closed.")
	}
	var l *lease
	if ref != "" {
		d := g.findDeviceLocked(ref)
		if d == nil {
			return nil, errResult(ErrInvalidArgument, "No phone matches %q. Connected phones: %s.", ref, g.deviceListLocked())
		}
		if s.barred[d.id] {
			return nil, statusResult(StatusStopped, "The person stopped the task on %s and its lease is gone. End the task.", d.name())
		}
		if l = s.leases[d.id]; l == nil {
			return nil, errResult(ErrInvalidArgument, "This session is not connected to %s; call phone_connect with device %q first.", d.name(), d.id)
		}
	} else {
		switch len(s.leases) {
		case 0:
			if len(s.barred) > 0 || s.stopped {
				return nil, statusResult(StatusStopped, "The person pressed Stop and the lease is gone. End the task.")
			}
			return nil, errResult(ErrInvalidArgument, "No phone is connected in this session; call phone_connect first.")
		case 1:
			for _, x := range s.leases {
				l = x
			}
		default:
			var names []string
			for _, x := range s.leases {
				names = append(names, fmt.Sprintf("%s (%s)", x.dev.name(), x.dev.id))
			}
			sort.Strings(names)
			return nil, errResult(ErrInvalidArgument, "This session holds %d phones: %s. Pass device.", len(names), strings.Join(names, ", "))
		}
	}
	l.inflight++
	l.lastUsed = time.Now()
	return l, nil
}

func (s *Session) done(l *lease) {
	s.g.mu.Lock()
	l.inflight--
	l.lastUsed = time.Now()
	s.g.mu.Unlock()
}

// finish turns any result of a lease the person stopped meanwhile into
// stopped, and fills in the device.
func (g *Gateway) finish(l *lease, r *Result) *Result {
	g.mu.Lock()
	ended, by := l.ended, l.endedBy
	name := l.dev.name()
	note := l.bgNote
	l.bgNote = ""
	g.mu.Unlock()
	if note != "" {
		r.Message = joinNote(r.Message, note)
	}
	if ended && by == OutcomeStopped {
		ran := r.Status == StatusSettled || r.Status == StatusUncertain
		r = statusResult(StatusStopped, "The person pressed Stop; the lease is gone. End the task.")
		if ran {
			r.Message += " The last action may have happened before the stop."
		}
	}
	if r.Device == "" {
		r.Device, r.DeviceName = l.dev.id, name
	}
	return r
}

func (g *Gateway) findDeviceLocked(ref string) *device {
	if d, ok := g.devices[ref]; ok {
		return d
	}
	var match []*device
	for _, d := range g.devices {
		if equalFold(d.name(), ref) {
			match = append(match, d)
		}
	}
	if len(match) == 0 && len(ref) >= 4 {
		for _, d := range g.devices {
			if hasPrefixFold(d.id, ref) {
				match = append(match, d)
			}
		}
	}
	if len(match) == 1 {
		return match[0]
	}
	return nil
}

func (g *Gateway) deviceListLocked() string {
	var out []string
	for _, d := range g.sortedDevicesLocked() {
		state := "online"
		if !d.online {
			state = "offline"
		}
		out = append(out, fmt.Sprintf("%s (device %q, %s)", d.name(), d.id, state))
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ", ")
}

// Connect implements phone_connect: it leases a device for a task and
// returns the first screen.
func (s *Session) Connect(ctx context.Context, req ConnectRequest) *Result {
	g := s.g
	title := strings.TrimSpace(req.Task)
	if title == "" {
		return errResult(ErrInvalidArgument, "task is required: one line naming the task, shown to the person.")
	}
	if r := []rune(title); len(r) > 200 {
		title = string(r[:200])
	}
	if res := s.approveAgent(ctx, req.Call, title, req.ReadOnly); res != nil {
		return res
	}

	g.mu.Lock()
	if s.closed {
		g.mu.Unlock()
		return errResult(ErrInternal, "This session is closed.")
	}
	var d *device
	if req.Device != "" {
		if d = g.findDeviceLocked(req.Device); d == nil {
			list := g.deviceListLocked()
			g.mu.Unlock()
			return errResult(ErrInvalidArgument, "No phone matches %q. Connected phones: %s.", req.Device, list)
		}
	} else if len(s.leases) == 1 {
		for _, l := range s.leases {
			d = l.dev
		}
	} else {
		var online []*device
		for _, x := range g.devices {
			if x.online {
				online = append(online, x)
			}
		}
		switch len(online) {
		case 0:
			g.mu.Unlock()
			return errResult(ErrDeviceOffline, "No phone is connected to Pinky. Ask the person to connect one.")
		case 1:
			d = online[0]
		default:
			list := g.deviceListLocked()
			g.mu.Unlock()
			return errResult(ErrInvalidArgument, "Several phones are connected: %s. Pass device.", list)
		}
	}
	if s.barred[d.id] || s.stopped {
		g.mu.Unlock()
		return statusResult(StatusStopped, "The person stopped this agent's task on %s; this session may not use it again. End the task.", d.name())
	}
	if !d.online {
		g.mu.Unlock()
		return errResult(ErrDeviceOffline, "%s is offline.", d.name())
	}
	if l := d.lease; l != nil && l.session != s {
		holder := fmt.Sprintf("%s · %s", l.session.agent.Name, l.task.title)
		g.mu.Unlock()
		res := errResult(ErrLeaseHeld, "%s is in use by %s.", d.name(), holder)
		res.Holder = holder
		return res
	}
	if l := d.lease; l != nil && l.task.title == title {
		// The same task again: keep the lease, refresh the app list. A
		// task that asked for read-only stays so.
		if len(req.Apps) > 0 {
			l.apps = append([]string(nil), req.Apps...)
		}
		if req.ReadOnly {
			l.readOnly, l.task.readOnly = true, true
		}
		l.inflight++
		ro := l.readOnly
		g.mu.Unlock()
		defer s.done(l)
		var unknown []string
		if len(req.Apps) > 0 {
			unknown = g.resolveTaskApps(ctx, l)
		}
		return readOnlyNote(withUnknownApps(g.firstScreen(ctx, l), unknown), ro)
	} else if l != nil {
		g.endLeaseLocked(l, OutcomeReplaced, "")
	}
	l := g.newLeaseLocked(s, d, title, req.Apps)
	l.readOnly, l.task.readOnly = req.ReadOnly, req.ReadOnly
	l.inflight++
	g.mu.Unlock()
	defer s.done(l)

	unknown := g.resolveTaskApps(ctx, l)
	g.recordSession(s)
	args := map[string]any{"task": title}
	if len(req.Apps) > 0 {
		args["apps"] = req.Apps
	}
	text := fmt.Sprintf("Connected to %s for “%s”", d.name(), title)
	if req.ReadOnly {
		args["read_only"] = true
		text += ", read only"
	}
	g.addStep(l, StepView{Tool: "phone_connect", Args: argsJSON(args), Text: text, Status: string(StatusSettled), At: time.Now()})
	g.emit(Event{Kind: EventTasks, DeviceID: d.id, TaskID: l.task.id})
	g.emit(Event{Kind: EventDevices, DeviceID: d.id})
	return readOnlyNote(withUnknownApps(g.firstScreen(ctx, l), unknown), req.ReadOnly)
}

// readOnlyNote tells an agent that asked for read-only what that means.
func readOnlyNote(res *Result, readOnly bool) *Result {
	if readOnly && !res.IsError() {
		res.Message = strings.TrimSpace(res.Message + " This task is read-only: anything the person's rules would ask about " +
			"is refused, and you can type only into search fields.")
	}
	return res
}

func (g *Gateway) newLeaseLocked(s *Session, d *device, title string, apps []string) *lease {
	now := time.Now()
	t := &task{id: history.NewID("t_"), sessionID: s.id, title: title, agent: s.agent.Name, deviceID: d.id,
		deviceName: d.name(), state: TaskWorking, started: now}
	l := &lease{id: history.NewID("l_"), session: s, dev: d, task: t, created: now, lastUsed: now,
		apps: append([]string(nil), apps...), grants: map[string]bool{}}
	l.ctx, l.cancel = context.WithCancel(g.ctx)
	// A watch-only agent watches the phone's own screen; a background
	// display would show it nothing.
	scope := g.opt.DefaultScope
	if st, ok := g.agents[s.agent.Name]; ok {
		scope = st.scope
	}
	l.bgWanted = g.backgroundLocked(d) && scope.Mode != ScopeWatchOnly
	d.lease = l
	if d.person {
		// The person is still in control from before: the task starts paused.
		t.state = TaskPaused
		l.tookOverSeen = true
	}
	g.noteWorkLocked(d)
	d.events = nil
	s.leases[d.id] = l
	s.used = appendUniq(s.used, d.id)
	g.recordTaskLocked(t)
	return l
}

func appendUniq(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

// resolveTaskApps adds the ids and names of installed apps that the task's
// allow-list names, so the list matches either. It returns the names that
// match no installed app, such as 天气 on a phone whose Weather app has an
// English name; nil when the phone cannot list its apps.
func (g *Gateway) resolveTaskApps(ctx context.Context, l *lease) []string {
	g.mu.Lock()
	want := append([]string(nil), l.apps...)
	g.mu.Unlock()
	if len(want) == 0 {
		return nil
	}
	dev := g.driverOf(l.dev)
	if dev == nil {
		return nil
	}
	apps, err := dev.Apps(ctx)
	if err != nil {
		return nil
	}
	out := append([]string(nil), want...)
	var unknown []string
	for _, w := range want {
		if a, ok := matchApp(apps, w); ok {
			out = appendUniq(out, a.ID)
			if a.Name != "" {
				out = appendUniq(out, a.Name)
			}
		} else {
			unknown = append(unknown, w)
		}
	}
	g.mu.Lock()
	l.apps = out
	g.mu.Unlock()
	return unknown
}

// withUnknownApps tells the agent at once which names in its task's app
// list match no installed app, rather than at the first refused launch.
func withUnknownApps(res *Result, unknown []string) *Result {
	if len(unknown) == 0 || res == nil || res.IsError() {
		return res
	}
	names := make([]string, len(unknown))
	for i, u := range unknown {
		names[i] = "“" + u + "”"
	}
	res.Message = strings.TrimSpace(res.Message + fmt.Sprintf(
		" No installed app matches %s in the task's app list, so opening those apps will be refused. Call phone_apps for the names this phone uses, then phone_connect again with the same task and the corrected apps.",
		strings.Join(names, ", ")))
	return res
}

// firstScreen reads the whole screen for a new lease.
func (g *Gateway) firstScreen(ctx context.Context, l *lease) *Result {
	ctx, cancel := mergeCtx(ctx, l.ctx)
	defer cancel()
	g.readyScreen(ctx, l)
	g.mu.Lock()
	g.startVideoLocked(l)
	g.mu.Unlock()
	cur, err := g.observe(ctx, l.dev, true)
	if err != nil {
		return g.finish(l, g.readError(l, err))
	}
	res := g.screenResult(l, cur, true)
	g.startShot(l.dev, cur)
	res.Status = StatusSettled
	res.Task = l.task.id
	res.Message = fmt.Sprintf("Connected to %s for “%s”.", l.dev.name(), l.task.title)
	g.mu.Lock()
	person := l.dev.person
	onBG := l.bg != nil
	g.mu.Unlock()
	if onBG {
		// A background display starts empty, without a home screen.
		res.Message += " This task runs on a background display of the phone, which starts empty and has no home screen: open apps with phone_launch."
	}
	switch {
	case person:
		res.Status = StatusPaused
		res.Message += " The person is in control of the phone; call phone_handoff to wait until they hand it back."
	case cur.Locked:
		res.Status = StatusLocked
		res.Message += " The phone is locked; call phone_handoff so the person can unlock it."
	}
	return g.finish(l, res)
}

// approveAgent holds an agent's first connection until the person allows
// it. It returns nil once the agent may connect.
func (s *Session) approveAgent(ctx context.Context, c Call, title string, readOnly bool) *Result {
	g := s.g
	if g.agentApproved(ctx, s.agent) {
		return nil
	}
	if g.opt.TrustNewAgents {
		g.allowAgent(s.agent)
		return nil
	}
	b := approval.Binding{Session: s.id, Tool: "phone_connect", Args: "agent"}
	id := c.ApprovalID
	if id != "" {
		if err := g.approvals.Check(id, b); err != nil {
			if errors.Is(err, approval.ErrMismatch) {
				return errResult(ErrInvalidArgument, "approval_id %s belongs to another call.", id)
			}
			id = ""
		}
	}
	if id == "" {
		for _, info := range g.approvals.List(true) {
			if info.Binding == b {
				id = info.ID
			}
		}
	}
	if id == "" {
		mode := g.opt.DefaultScope.Mode
		if readOnly {
			mode = ScopeReadOnly
		}
		info := g.approvals.Open(approval.Request{
			Kind: approval.KindAgent, Binding: b, Rule: "New agent", Agent: s.agent.Name,
			Title:  fmt.Sprintf("Let %s use your phones?", s.agent.Name),
			Detail: fmt.Sprintf("%s wants to connect for “%s”. Allowing gives it %s access.", s.agent.Name, title, mode),
			Data:   agentRequest{readOnly: readOnly},
		})
		id = info.ID
	}
	info, _ := g.approvals.Get(id)
	out, err := g.waitApproval(ctx, nil, id, info, c.Progress)
	if err != nil {
		g.approvals.Withdraw(id, "the agent cancelled the call")
		return errResult(ErrInternal, "The call was cancelled.")
	}
	switch {
	case out.State == approval.Open:
		return &Result{Status: StatusPending, ApprovalID: id, Rule: "New agent",
			Message: fmt.Sprintf("This is %s's first connection, and the person must allow it in Pinky. Repeat phone_connect with approval_id %q.", s.agent.Name, id)}
	case out.State == approval.Used && out.Decision.Allows():
		g.allowAgent(s.agent)
		return nil
	}
	return &Result{Status: StatusDenied, Rule: "New agent", Note: denyNote(out),
		Message: "The person did not allow this agent to use Pinky."}
}

func denyNote(out approval.Outcome) string {
	switch {
	case out.Note != "":
		return out.Note
	case out.TimedOut:
		return "No answer."
	case out.State == approval.Withdrawn && out.Reason != "":
		return "Withdrawn: " + out.Reason + "."
	}
	return ""
}

// agentState returns what the person allowed an agent, loading it from
// history the first time.
func (g *Gateway) agentState(ctx context.Context, a AgentInfo) *agentState {
	g.mu.Lock()
	st, ok := g.agents[a.Name]
	g.mu.Unlock()
	if ok {
		return st
	}
	st = &agentState{name: a.Name, scope: g.opt.DefaultScope}
	if g.store != nil {
		if rec, found, err := g.store.Agent(ctx, a.Name); err == nil && found {
			st.approved = !rec.ApprovedAt.IsZero()
			st.scope = ParseScope(rec.Scope)
		} else if err == nil {
			now := time.Now()
			_ = g.store.SaveAgent(ctx, history.Agent{Name: a.Name, Client: a.Client, Scope: st.scope.String(), CreatedAt: now, LastSeen: now})
		}
	}
	g.mu.Lock()
	if cur, ok := g.agents[a.Name]; ok {
		st = cur
	} else {
		g.agents[a.Name] = st
	}
	g.mu.Unlock()
	return st
}

// agentApproved reports whether the person approved the agent.
func (g *Gateway) agentApproved(ctx context.Context, a AgentInfo) bool {
	st := g.agentState(ctx, a)
	g.mu.Lock()
	defer g.mu.Unlock()
	return st.approved
}

// setAgentApproved approves an agent with scope.
func (g *Gateway) setAgentApproved(a AgentInfo, scope Scope) {
	g.approveAgentWith(a, func(*agentState) Scope { return scope })
}

// allowAgent approves an agent whose first connection the person allowed.
// A scope the person chose meanwhile is kept: the client calls
// SetAgentScope, which also approves, right before Decide. Otherwise the
// agent gets DefaultScope.
func (g *Gateway) allowAgent(a AgentInfo) {
	g.approveAgentWith(a, func(st *agentState) Scope {
		if st.approved {
			return st.scope
		}
		return g.opt.DefaultScope
	})
}

// approveAgentWith approves an agent with the scope pick returns, chosen
// and saved under the lock so a concurrent SetAgentScope cannot be undone
// by a stale value.
func (g *Gateway) approveAgentWith(a AgentInfo, pick func(*agentState) Scope) {
	g.mu.Lock()
	defer g.mu.Unlock()
	st, ok := g.agents[a.Name]
	if !ok {
		st = &agentState{name: a.Name}
		g.agents[a.Name] = st
	}
	scope := pick(st)
	st.approved, st.scope = true, scope
	if g.store == nil {
		return
	}
	// Queued under the lock, so history keeps the order of the changes.
	g.rec.do(func(ctx context.Context) error {
		now := time.Now()
		rec, found, err := g.store.Agent(ctx, a.Name)
		if err != nil {
			return err
		}
		if !found {
			rec = history.Agent{Name: a.Name, CreatedAt: now}
		}
		if a.Client != "" {
			rec.Client = a.Client
		}
		rec.Scope, rec.ApprovedAt, rec.LastSeen = scope.String(), now, now
		return g.store.SaveAgent(ctx, rec)
	})
}

func (g *Gateway) scopeOf(s *Session) Scope {
	g.mu.Lock()
	defer g.mu.Unlock()
	if st, ok := g.agents[s.agent.Name]; ok {
		return st.scope
	}
	return g.opt.DefaultScope
}

// Release implements phone_release.
func (s *Session) Release(ctx context.Context, req ReleaseRequest) *Result {
	g := s.g
	outcome := strings.ToLower(strings.TrimSpace(req.Outcome))
	switch outcome {
	case "":
		outcome = OutcomeDone
	case OutcomeDone, OutcomeFailed, "partial", "cancelled", "canceled":
	default:
		return errResult(ErrInvalidArgument, "outcome must be done, failed, partial or cancelled.")
	}
	if outcome == "canceled" {
		outcome = "cancelled"
	}
	g.mu.Lock()
	var ls []*lease
	if req.Device != "" {
		d := g.findDeviceLocked(req.Device)
		if d == nil || s.leases[d.id] == nil {
			g.mu.Unlock()
			return errResult(ErrInvalidArgument, "This session does not hold %q.", req.Device)
		}
		ls = append(ls, s.leases[d.id])
	} else {
		for _, l := range s.leases {
			ls = append(ls, l)
		}
	}
	var names []string
	for _, l := range ls {
		names = append(names, l.dev.name())
		g.addStepLocked(l.task, StepView{Tool: "phone_release", Args: argsJSON(map[string]any{"outcome": outcome, "summary": req.Summary}),
			Text: "Finished: " + outcome, Status: string(StatusSettled), At: time.Now()}, "", "")
		g.endLeaseLocked(l, outcome, req.Summary)
	}
	g.mu.Unlock()
	if len(ls) == 0 {
		return &Result{Status: StatusSettled, Message: "Nothing to release."}
	}
	sort.Strings(names)
	res := &Result{Status: StatusSettled, Message: fmt.Sprintf("Released %s.", strings.Join(names, ", "))}
	if len(ls) == 1 {
		res.Device, res.DeviceName = ls[0].dev.id, ls[0].dev.name()
	}
	return res
}

// Observe implements phone_observe.
func (s *Session) Observe(ctx context.Context, req ObserveRequest) *Result {
	if req.WaitMS < 0 || time.Duration(req.WaitMS)*time.Millisecond > MaxObserveWait {
		return errResult(ErrInvalidArgument, "wait_ms must be between 0 and 30000.")
	}
	l, res := s.acquire(req.Device)
	if res != nil {
		return res
	}
	defer s.done(l)
	g := s.g
	ctx, cancel := mergeCtx(ctx, l.ctx)
	defer cancel()
	if res := g.gate(l, req.ApprovalID, false); res != nil {
		return g.finish(l, res)
	}
	g.readyScreen(ctx, l)
	if req.WaitMS > 0 {
		g.waitChange(ctx, l, time.Duration(req.WaitMS)*time.Millisecond, req.Progress)
	}
	cur, err := g.observe(ctx, l.dev, true)
	if err != nil {
		return g.finish(l, g.readError(l, err))
	}
	res = g.screenResult(l, cur, true)
	res.Status = StatusSettled
	if cur.Locked {
		res.Status = StatusLocked
		res.Message = "The phone is locked; call phone_handoff so the person can unlock it."
	}
	_, blocked := g.policy.Blocked(cur.App)
	if !req.Screenshot {
		g.startShot(l.dev, cur)
	} else {
		switch {
		case res.Rule != "":
			res.Message = strings.TrimSpace(res.Message + " No screenshot: the app is blocked.")
		case blocked:
			res.Message = strings.TrimSpace(res.Message + " No screenshot: Pinky takes no pictures of a blocked app, also when the person allows it for the task.")
		default:
			img, err := g.screenshot(ctx, l.dev, cur)
			switch {
			case err == nil:
				res.Screenshot = img
				// Checked against reads on both sides: steps may reuse it.
				g.keepImage(l.dev, cur, img)
			case errors.Is(err, driver.ErrUnsupported):
				res.Message = strings.TrimSpace(res.Message + " This phone cannot take screenshots.")
			case errors.Is(err, errLockedShot):
				res.Message = strings.TrimSpace(res.Message + " No screenshot: " + err.Error() + ".")
			case errors.Is(err, errScreenChanged):
				res.Message = strings.TrimSpace(res.Message + " No screenshot: the screen changed while it was taken; observe again.")
			default:
				res.Message = strings.TrimSpace(res.Message + " The screenshot failed: " + err.Error())
			}
		}
	}
	return g.finish(l, res)
}

// waitChange waits until the screen differs from what the agent last saw
// and settles, or until wait passes.
func (g *Gateway) waitChange(ctx context.Context, l *lease, wait time.Duration, progress Progress) {
	g.mu.Lock()
	base := l.baseline
	wake := l.dev.wake
	g.mu.Unlock()
	start := time.Now()
	deadline := start.Add(wait)
	lastProgress := start
	for time.Now().Before(deadline) {
		if !sleepCtx(ctx, g.opt.PollInterval, wake) {
			return
		}
		cur, err := g.observe(ctx, l.dev, false)
		if err != nil {
			return
		}
		view, _ := g.agentView(l, cur)
		if base == nil || body(view) != body(base) {
			g.settle(ctx, l.dev)
			return
		}
		if progress != nil && time.Since(lastProgress) >= 5*time.Second {
			lastProgress = time.Now()
			progress(time.Since(start).Seconds(), wait.Seconds(), "Waiting for the screen to change")
		}
	}
}

// WaitFor implements phone_wait_for.
func (s *Session) WaitFor(ctx context.Context, req WaitForRequest) *Result {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return errResult(ErrInvalidArgument, "text is required.")
	}
	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	switch {
	case req.TimeoutMS < 0 || timeout > MaxWaitFor:
		return errResult(ErrInvalidArgument, "timeout_ms must be between 0 and 45000.")
	case timeout == 0:
		timeout = 10 * time.Second
	}
	l, res := s.acquire(req.Device)
	if res != nil {
		return res
	}
	defer s.done(l)
	g := s.g
	ctx, cancel := mergeCtx(ctx, l.ctx)
	defer cancel()
	if res := g.gate(l, req.ApprovalID, false); res != nil {
		return g.finish(l, res)
	}
	g.readyScreen(ctx, l)
	start := time.Now()
	deadline := start.Add(timeout)
	lastProgress := start
	var cur *screen.Screen
	matched := false
	// Text that only the pixels show (a web view, a canvas) is in OCR
	// elements, which tree reads leave out: read with OCR as well, at most
	// once a second, and count its answer until the next such read.
	var lastOCR time.Time
	ocrFound := false
	for {
		var err error
		cur, err = g.observe(ctx, l.dev, false)
		if err != nil {
			return g.finish(l, g.readError(l, err))
		}
		view, _ := g.agentView(l, cur)
		g.mu.Lock()
		evs := append([]screen.Event(nil), l.dev.events...)
		g.mu.Unlock()
		found := screenHas(view, evs, text)
		if !found && time.Since(lastOCR) >= waitForOCREvery {
			if o, err := g.observe(ctx, l.dev, true); err == nil {
				lastOCR = time.Now()
				ov, _ := g.agentView(l, o)
				ocrFound = screenHas(ov, evs, text)
				if ocrFound || req.Gone {
					cur = o
				}
			}
		}
		found = found || ocrFound
		if found != req.Gone {
			matched = true
			break
		}
		if !time.Now().Before(deadline) {
			break
		}
		g.mu.Lock()
		wake := l.dev.wake
		g.mu.Unlock()
		if !sleepCtx(ctx, g.opt.PollInterval, wake) {
			if ctx.Err() != nil {
				return g.finish(l, errResult(ErrInternal, "The call was cancelled."))
			}
		}
		if req.Progress != nil && time.Since(lastProgress) >= 5*time.Second {
			lastProgress = time.Now()
			req.Progress(time.Since(start).Seconds(), timeout.Seconds(), fmt.Sprintf("Waiting for “%s”", text))
		}
	}
	res = g.screenResult(l, cur, false)
	g.startShot(l.dev, cur)
	res.Status = StatusSettled
	res.Matched = &matched
	verb := "appeared"
	if req.Gone {
		verb = "disappeared"
	}
	if matched {
		res.Message = fmt.Sprintf("“%s” %s after %s.", text, verb, approval.Human(time.Since(start)))
	} else {
		res.Message = fmt.Sprintf("“%s” has not %s after %s.", text, verb, approval.Human(timeout))
	}
	g.addStep(l, StepView{Tool: "phone_wait_for", Args: argsJSON(map[string]any{"text": text, "gone": req.Gone, "timeout_ms": req.TimeoutMS}),
		Text: fmt.Sprintf("Waited for “%s”", text), Status: string(StatusSettled), At: start, Elapsed: time.Since(start)})
	return g.finish(l, res)
}

// screenHas reports whether an element's label or an event shows text
// (containsText).
func screenHas(s *screen.Screen, evs []screen.Event, text string) bool {
	for _, e := range s.Elements {
		if containsText(e.Label, text) {
			return true
		}
	}
	for _, ev := range evs {
		if containsText(ev.Text, text) {
			return true
		}
	}
	return false
}

// containsText reports whether label contains text, ignoring case. Where a
// script puts spaces between words, the text must match whole words, so
// waiting for "Stop" is not answered by the "Stopwatch" tab already on the
// screen; Chinese, Japanese, Korean and Thai text matches anywhere, as
// their words run together.
func containsText(label, text string) bool {
	l, t := strings.ToLower(label), strings.ToLower(text)
	if t == "" {
		return false
	}
	first, _ := utf8.DecodeRuneInString(t)
	last, _ := utf8.DecodeLastRuneInString(t)
	for i := 0; i <= len(l); {
		j := strings.Index(l[i:], t)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(t)
		before, _ := utf8.DecodeLastRuneInString(l[:start])
		after, _ := utf8.DecodeRuneInString(l[end:])
		if !(wordRune(before) && wordRune(first)) && !(wordRune(last) && wordRune(after)) {
			return true
		}
		_, size := utf8.DecodeRuneInString(l[start:])
		i = start + size
	}
	return false
}

// wordRune reports whether r is a letter or digit of a script that
// separates its words with spaces.
func wordRune(r rune) bool {
	if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
		return false
	}
	return !unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul,
		unicode.Thai, unicode.Lao, unicode.Khmer, unicode.Myanmar)
}

// Apps implements phone_apps: installed apps inside the agent's scope.
// Blocked apps are not listed.
func (s *Session) Apps(ctx context.Context, req AppsRequest) *Result {
	l, res := s.acquire(req.Device)
	if res != nil {
		return res
	}
	defer s.done(l)
	g := s.g
	ctx, cancel := mergeCtx(ctx, l.ctx)
	defer cancel()
	if res := g.gate(l, req.ApprovalID, false); res != nil {
		return g.finish(l, res)
	}
	dev := g.driverOf(l.dev)
	if dev == nil {
		return g.finish(l, errResult(ErrDeviceOffline, "%s is offline.", l.dev.name()))
	}
	apps, err := dev.Apps(ctx)
	if err != nil {
		return g.finish(l, g.readError(l, err))
	}
	scope := g.scopeOf(s)
	q := strings.ToLower(strings.TrimSpace(req.Query))
	var out []driver.App
	for _, a := range apps {
		app := screen.App{ID: a.ID, Name: a.Name}
		if _, blocked := g.policy.Blocked(app); blocked {
			continue
		}
		if scope.Mode == ScopeApps && !g.policy.IsSystem(app) && !appListed(app, scope.Apps) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(a.Name), q) && !strings.Contains(strings.ToLower(a.ID), q) {
			continue
		}
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].System != out[j].System {
			return !out[i].System
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	res = &Result{Status: StatusSettled, Apps: out}
	if len(out) == 0 {
		res.Message = "No apps match."
	}
	return g.finish(l, res)
}

func appListed(app screen.App, list []string) bool {
	for _, x := range list {
		if x == app.ID || (app.Name != "" && strings.EqualFold(x, app.Name)) {
			return true
		}
	}
	return false
}

// RequestApproval implements phone_request_approval: it asks the person a
// question and records the answer. It grants nothing: a later step the
// rules ask about still asks, and so does the next question, since a yes answers this one only
// (canAllowTask).
func (s *Session) RequestApproval(ctx context.Context, req ApprovalRequest) *Result {
	summary := strings.TrimSpace(req.Summary)
	if summary == "" {
		return errResult(ErrInvalidArgument, "summary is required.")
	}
	l, res := s.acquire(req.Device)
	if res != nil {
		return res
	}
	defer s.done(l)
	g := s.g
	ctx, cancel := mergeCtx(ctx, l.ctx)
	defer cancel()
	if res := g.gate(l, req.ApprovalID, true); res != nil {
		return g.finish(l, res)
	}
	b := approval.Binding{Session: s.id, Device: l.dev.id, Lease: l.id, Tool: "phone_request_approval",
		Args: digest(summary + "\x00" + req.Details)}
	id := req.ApprovalID
	if id != "" {
		if res := g.checkApproval(id, b); res != nil {
			if res.Error != "" {
				return g.finish(l, res)
			}
			id = ""
		}
	}
	var info approval.Info
	if id == "" {
		var shot image.Image
		g.mu.Lock()
		last := l.dev.last
		g.mu.Unlock()
		if last != nil && time.Since(last.CapturedAt) < 10*time.Second {
			shot = g.rawShot(ctx, l.dev, last)
		}
		detail := s.agent.Name
		if d := strings.TrimSpace(req.Details); d != "" {
			detail += " · " + d
		}
		info = g.approvals.Open(approval.Request{Kind: approval.KindRequest, Binding: b, Rule: "Agent request",
			Title: summary, Detail: detail, Agent: s.agent.Name, TaskID: l.task.id, DeviceName: l.dev.name(),
			App: g.appName(l.dev), Screenshot: shot})
		if res := g.afterOpen(l, info); res != nil {
			return g.finish(l, res)
		}
		g.openStep(l, info, "phone_request_approval", argsJSON(map[string]any{"summary": summary, "details": req.Details}),
			fmt.Sprintf("Asked you: “%s”", summary), screen.Rect{}, screen.Point{}, "")
	} else {
		info, _ = g.approvals.Get(id)
	}
	out, err := g.waitApproval(ctx, l, info.ID, info, req.Progress)
	if errors.Is(err, errTaken) {
		return g.finish(l, takenResult(info.ID))
	}
	if err != nil {
		return g.finish(l, g.cancelled(l, info.ID))
	}
	switch {
	case out.State == approval.Open:
		return g.finish(l, g.pendingResult(l, info))
	case out.State == approval.Withdrawn:
		g.closeStep(info.ID, string(StatusDenied), 0)
		return g.finish(l, g.withdrawnResult(l, out))
	}
	g.recordDecision(info, out)
	if out.Decision.Allows() {
		g.closeStep(info.ID, string(StatusSettled), out.Waited)
		res := &Result{Status: StatusSettled, Decision: "allow", Note: out.Note,
			Message: "The person said yes. This grants nothing by itself: a later step their rules ask about, such as a payment, still asks."}
		return g.finish(l, res)
	}
	g.closeStep(info.ID, string(StatusDenied), out.Waited)
	return g.finish(l, &Result{Status: StatusDenied, Decision: "deny", Note: denyNote(out), Message: "The person said no."})
}

// Handoff implements phone_handoff: the agent waits until the person hands
// control back, with their note.
func (s *Session) Handoff(ctx context.Context, req HandoffRequest) *Result {
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return errResult(ErrInvalidArgument, "reason is required: tell the person what you need.")
	}
	l, res := s.acquire(req.Device)
	if res != nil {
		return res
	}
	defer s.done(l)
	g := s.g
	ctx, cancel := mergeCtx(ctx, l.ctx)
	defer cancel()
	if res := g.gate(l, req.ApprovalID, false); res != nil {
		return g.finish(l, res)
	}
	b := approval.Binding{Session: s.id, Device: l.dev.id, Lease: l.id, Tool: "phone_handoff"}
	g.mu.Lock()
	if l.handedBack && req.ApprovalID == "" {
		note := l.handBackNote
		l.handedBack, l.handBackNote = false, ""
		g.mu.Unlock()
		return g.handedBackResult(ctx, l, note)
	}
	g.mu.Unlock()

	id := req.ApprovalID
	if id != "" {
		if res := g.checkApproval(id, b); res != nil {
			if res.Error != "" {
				return g.finish(l, res)
			}
			id = ""
		}
	}
	if id == "" {
		for _, info := range g.approvals.List(true) {
			if info.Binding == b {
				id = info.ID
			}
		}
	}
	var info approval.Info
	if id == "" {
		// Unlocking from the computer answers a handoff opened on a locked
		// phone only when unlocking is all it asks for.
		g.mu.Lock()
		endsOnUnlock := l.dev.locked && unlockOnly(reason)
		g.mu.Unlock()
		info = g.approvals.Open(approval.Request{Kind: approval.KindHandoff, Binding: b, Rule: "Handoff",
			Title: reason, Detail: fmt.Sprintf("%s · %s", s.agent.Name, l.task.title), Agent: s.agent.Name,
			TaskID: l.task.id, DeviceName: l.dev.name(), App: g.appName(l.dev), Timeout: g.opt.HandoffTimeout})
		g.mu.Lock()
		if endsOnUnlock {
			l.unlockHandoffs = append(l.unlockHandoffs, info.ID)
		}
		g.mu.Unlock()
		if endsOnUnlock {
			// An unlock from the computer as soon as the card showed may
			// have finished before the line above and found nothing to
			// answer: answer now if the phone is unlocked.
			g.answerUnlockHandoffs(l.dev)
		}
		if res := g.afterOpen(l, info); res != nil {
			return g.finish(l, res)
		}
		g.openStep(l, info, "phone_handoff", argsJSON(map[string]any{"reason": reason}),
			fmt.Sprintf("Asked you to take over: %s", reason), screen.Rect{}, screen.Point{}, "")
	} else {
		info, _ = g.approvals.Get(id)
	}
	out, err := g.waitApproval(ctx, l, info.ID, info, req.Progress)
	if errors.Is(err, errTaken) {
		return g.finish(l, takenResult(info.ID))
	}
	if err != nil {
		return g.finish(l, g.cancelled(l, info.ID))
	}
	switch {
	case out.State == approval.Open:
		return g.finish(l, g.pendingResult(l, info))
	case out.State == approval.Withdrawn:
		g.closeStep(info.ID, string(StatusDenied), 0)
		return g.finish(l, g.withdrawnResult(l, out))
	}
	g.recordDecision(info, out)
	if out.Decision == approval.Deny {
		g.closeStep(info.ID, string(StatusDenied), out.Waited)
		return g.finish(l, &Result{Status: StatusDenied, Note: denyNote(out), Message: "The person declined the handoff."})
	}
	g.closeStep(info.ID, string(StatusSettled), out.Waited)
	g.mu.Lock()
	l.handedBack, l.handBackNote = false, ""
	g.mu.Unlock()
	return g.handedBackResult(ctx, l, out.Note)
}

func (g *Gateway) handedBackResult(ctx context.Context, l *lease, note string) *Result {
	g.readyScreen(ctx, l)
	cur, err := g.observe(ctx, l.dev, true)
	if err != nil {
		return g.finish(l, g.readError(l, err))
	}
	res := g.screenResult(l, cur, true)
	g.startShot(l.dev, cur)
	res.Status = StatusSettled
	res.Note = note
	res.Message = "The person handed control back."
	if cur.Locked {
		res.Status = StatusLocked
		res.Message = "The person handed control back, but the phone is locked."
	}
	return g.finish(l, res)
}

// Batch implements phone_batch: steps run in order and stop at the first
// one that does not settle.
func (s *Session) Batch(ctx context.Context, req BatchRequest) *Result {
	if len(req.Steps) == 0 {
		return errResult(ErrInvalidArgument, "steps is empty.")
	}
	if len(req.Steps) > MaxBatchSteps {
		return errResult(ErrInvalidArgument, "phone_batch takes at most %d steps.", MaxBatchSteps)
	}
	for i, a := range req.Steps {
		if res := a.check(); res != nil {
			res.Message = fmt.Sprintf("step %d: %s", i+1, res.Message)
			return res
		}
	}
	l, res := s.acquire(req.Device)
	if res != nil {
		return res
	}
	defer s.done(l)
	g := s.g
	ctx, cancel := mergeCtx(ctx, l.ctx)
	defer cancel()
	out := &Result{Status: StatusSettled}
	for i, a := range req.Steps {
		c := Call{Device: req.Device, Intent: req.Intent, ApprovalID: a.ApprovalID, Progress: req.Progress, reuse: i > 0}
		if a.Intent != "" {
			c.Intent = a.Intent
		}
		r := g.finish(l, g.act(ctx, l, c, a))
		out.Steps = append(out.Steps, r)
		if r.IsError() || r.Status != StatusSettled {
			break
		}
	}
	last := out.Steps[len(out.Steps)-1]
	out.Status, out.Error, out.ApprovalID, out.Rule, out.Note = last.Status, last.Error, last.ApprovalID, last.Rule, last.Note
	out.Screen = last.Screen
	if n, total := len(out.Steps), len(req.Steps); n < total {
		out.Message = fmt.Sprintf("Stopped at step %d of %d.", n, total)
	}
	return g.finish(l, out)
}

func sleepCtx(ctx context.Context, d time.Duration, wake <-chan struct{}) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	case <-wake:
		return false
	}
}
