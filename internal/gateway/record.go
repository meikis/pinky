package gateway

import (
	"context"
	"image"
	"time"

	"github.com/DIYgod/pinky/internal/approval"
	"github.com/DIYgod/pinky/internal/history"
	"github.com/DIYgod/pinky/internal/screen"
)

// reasonTookOver withdraws approvals when the person takes over.
const reasonTookOver = "took_over"

// rawShot takes a screenshot of the screen s describes, for the approval
// panel, which the person needs at once: s must come from a read just
// before, and a blocked app is never captured. It costs no extra read,
// unlike screenshot, which agents get. History screenshots are taken in
// the background instead (shots.go).
func (g *Gateway) rawShot(ctx context.Context, d *device, s *screen.Screen) image.Image {
	if s == nil {
		return nil
	}
	if _, blocked := g.policy.Blocked(s.App); blocked {
		return nil
	}
	dev, bg := g.screenOf(d)
	if dev == nil {
		return nil
	}
	img, err := g.shotOf(ctx, d, dev, bg)
	if err != nil {
		return nil
	}
	return img
}

// addStep appends a step to the lease's task.
func (g *Gateway) addStep(l *lease, st StepView) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.addStepLocked(l.task, st, "", "")
}

// addStepLocked appends a step and queues its history write.
func (g *Gateway) addStepLocked(t *task, st StepView, intent, screenText string) int {
	st.Index = t.next
	t.next++
	t.stepCount++
	t.steps = append(t.steps, st)
	if n := len(t.steps); n > maxTaskSteps {
		t.steps = append([]StepView(nil), t.steps[n-maxTaskSteps:]...)
	}
	g.saveStepLocked(t, st, intent, screenText)
	g.recordTaskLocked(t)
	g.emit(Event{Kind: EventTasks, DeviceID: t.deviceID, TaskID: t.id})
	return st.Index
}

// updateStepLocked replaces a step that is still in memory.
func (g *Gateway) updateStepLocked(t *task, st StepView, intent, screenText string) {
	for i := len(t.steps) - 1; i >= 0; i-- {
		if t.steps[i].Index == st.Index {
			if st.Before == "" {
				st.Before = t.steps[i].Before
			}
			if st.After == "" {
				st.After = t.steps[i].After
			}
			t.steps[i] = st
			break
		}
	}
	g.saveStepLocked(t, st, intent, screenText)
	g.emit(Event{Kind: EventTasks, DeviceID: t.deviceID, TaskID: t.id})
}

func (g *Gateway) saveStepLocked(t *task, st StepView, intent, screenText string) {
	if g.store == nil {
		return
	}
	rec := history.Step{TaskID: t.id, Index: st.Index, Tool: st.Tool, Args: st.Args, Intent: intent, Text: st.Text,
		Status: st.Status, Started: st.At, Elapsed: st.Elapsed, Before: st.Before, After: st.After,
		ScreenText: screenText, Target: st.Target, Point: st.Point}
	g.rec.do(func(ctx context.Context) error { return g.store.SaveStep(ctx, rec) })
}

// recordStep records an executed (or failed) action and returns its index.
// step is the index of a step an approval opened, or -1 for a new one. An
// action that ran, or may have, without asking because of the person's
// Always allow says so on a second line (exemptedLine).
func (g *Gateway) recordStep(l *lease, step int, a Action, c Call, p *prep, res *Result, start time.Time, before, after, screenText string) int {
	status := string(res.Status)
	if res.Error != "" {
		status = string(res.Error)
	}
	st := StepView{Text: describe(a, p), Tool: "phone_" + a.Tool, Args: a.args(c), Status: status, At: start,
		Elapsed: time.Since(start), Before: before, After: after}
	if p != nil {
		if p.target != nil {
			st.Target = p.target.Bounds
		}
		if p.hasPt {
			st.Point = p.point
		} else if a.Tool == "swipe" {
			st.Point = p.from
		}
		if len(p.exempted) > 0 && (res.Status == StatusSettled || res.Status == StatusUncertain) {
			st.Text += "\n" + exemptedLine(p)
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if step >= 0 {
		st.Index = step
		g.updateStepLocked(l.task, st, c.Intent, screenText)
		return step
	}
	return g.addStepLocked(l.task, st, c.Intent, screenText)
}

// recordRefusal records an action that policy or the screen refused.
func (g *Gateway) recordRefusal(l *lease, c Call, a Action, p *prep, res *Result) {
	if p == nil {
		p = &prep{}
	}
	text := describe(a, p)
	switch {
	case res.Status == StatusBlocked:
		text = "Blocked by “" + res.Rule + "”: " + text
	case res.Error == ErrScopeDenied:
		text = "Outside the scope: " + text
	case res.Error != "":
		text = "Failed: " + text
	}
	status := string(res.Status)
	if res.Error != "" {
		status = string(res.Error)
	}
	st := StepView{Text: text, Tool: "phone_" + a.Tool, Args: a.args(c), Status: status, At: time.Now()}
	if p.target != nil {
		st.Target = p.target.Bounds
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.addStepLocked(l.task, st, c.Intent, "")
}

// openStep records the step an approval holds and remembers it, so the call
// that picks up the decision finishes the same step.
func (g *Gateway) openStep(l *lease, info approval.Info, tool, args, text string, target screen.Rect, point screen.Point, before string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	t := l.task
	idx := g.addStepLocked(t, StepView{Text: text, Tool: tool, Args: args, Status: string(StatusPending), At: info.Created,
		Before: before, Target: target, Point: point}, "", "")
	t.approvals++
	if t.ended.IsZero() && t.state == TaskWorking {
		t.state = TaskWaiting
	}
	g.apSteps[info.ID] = stepRef{task: t, index: idx, tool: tool, args: args, start: info.Created}
	g.recordTaskLocked(t)
	if g.store != nil {
		rec := history.Approval{ID: info.ID, TaskID: t.id, StepIndex: idx, Kind: string(info.Kind), Rule: info.Rule,
			Title: info.Title, Created: info.Created}
		g.rec.do(func(ctx context.Context) error { return g.store.SaveApproval(ctx, rec) })
	}
}

// closeStep sets the final status of a step an approval held, when the
// action did not run.
func (g *Gateway) closeStep(apID, status string, waited time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	ref, ok := g.apSteps[apID]
	if !ok {
		return
	}
	delete(g.apSteps, apID)
	t := ref.task
	for i := len(t.steps) - 1; i >= 0; i-- {
		if t.steps[i].Index == ref.index {
			st := t.steps[i]
			st.Status = status
			st.Elapsed = time.Since(ref.start)
			g.updateStepLocked(t, st, "", "")
			break
		}
	}
	if t.state == TaskWaiting && t.ended.IsZero() {
		t.state = TaskWorking
		g.recordTaskLocked(t)
	}
}

// closePendingSteps closes the steps of approvals that were withdrawn.
func (g *Gateway) closePendingSteps(infos []approval.Info, status string) {
	for _, info := range infos {
		g.recordDecision(info, approval.Outcome{State: approval.Withdrawn, Reason: status})
		g.closeStep(info.ID, status, 0)
	}
}

// approvalExpired closes the step of an approval whose decision no call
// picked up, so history does not show it waiting forever.
func (g *Gateway) approvalExpired(info approval.Info) {
	out := approval.Outcome{State: approval.Used, Decision: info.Decision, Note: info.Note, TimedOut: info.TimedOut,
		Waited: info.DecidedAt.Sub(info.Created)}
	g.recordDecision(info, out)
	status := string(StatusDenied)
	if info.Decision.Allows() {
		status = "expired"
	}
	g.closeStep(info.ID, status, out.Waited)
}

// recordDecision stores the person's answer.
func (g *Gateway) recordDecision(info approval.Info, out approval.Outcome) {
	if g.store == nil {
		return
	}
	g.mu.Lock()
	ref, ok := g.apSteps[info.ID]
	g.mu.Unlock()
	idx := -1
	if ok {
		idx = ref.index
	}
	decision := string(out.Decision)
	switch {
	case out.TimedOut:
		decision = "timeout"
	case out.State == approval.Withdrawn:
		decision = "withdrawn"
	}
	rec := history.Approval{ID: info.ID, TaskID: info.TaskID, StepIndex: idx, Kind: string(info.Kind), Rule: info.Rule,
		Title: info.Title, Decision: decision, Note: out.Note, Created: info.Created, Decided: time.Now(), Waited: out.Waited}
	g.rec.do(func(ctx context.Context) error { return g.store.SaveApproval(ctx, rec) })
}

func (g *Gateway) setTaskState(l *lease, st TaskState) {
	g.mu.Lock()
	defer g.mu.Unlock()
	t := l.task
	if !t.ended.IsZero() || t.state == st {
		return
	}
	t.state = st
	g.recordTaskLocked(t)
	g.emit(Event{Kind: EventTasks, DeviceID: t.deviceID, TaskID: t.id})
	g.emit(Event{Kind: EventDevices, DeviceID: t.deviceID})
}

func (g *Gateway) recordTaskLocked(t *task) {
	if g.store == nil {
		return
	}
	rec := history.Task{ID: t.id, SessionID: t.sessionID, DeviceID: t.deviceID, DeviceName: t.deviceName,
		Agent: t.agent, Title: t.title, State: string(t.state), Outcome: t.outcome, Summary: t.summary,
		Started: t.started, Ended: t.ended, StepCount: t.stepCount, ApprovalCount: t.approvals}
	g.rec.do(func(ctx context.Context) error { return g.store.SaveTask(ctx, rec) })
}

func (g *Gateway) recordSession(s *Session) {
	if g.store == nil {
		return
	}
	g.mu.Lock()
	rec := history.Session{ID: s.id, Agent: s.agent.Name, Devices: append([]string(nil), s.used...), Started: s.started}
	if s.closed {
		rec.Ended, rec.EndReason = time.Now(), OutcomeDisconnected
	}
	g.mu.Unlock()
	g.rec.do(func(ctx context.Context) error { return g.store.SaveSession(ctx, rec) })
}

// endLease ends a lease and its task.
func (g *Gateway) endLease(l *lease, outcome, summary string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.endLeaseLocked(l, outcome, summary)
}

// endLeaseLocked ends a lease: the task ends with outcome, open approvals
// are withdrawn and in-flight driver calls are cancelled.
func (g *Gateway) endLeaseLocked(l *lease, outcome, summary string) {
	if l.ended {
		return
	}
	l.ended, l.endedBy = true, outcome
	d := l.dev
	if d.lease == l {
		// Person control stays with the device until HandBack.
		d.lease = nil
		d.events = nil
		if d.inflight != nil {
			d.inflight()
		}
		g.noteWorkLocked(d)
	}
	if l.session.leases[d.id] == l {
		delete(l.session.leases, d.id)
	}
	if outcome == OutcomeStopped {
		l.session.barred[d.id] = true
	}
	l.cancel()
	t := l.task
	t.state = stateForOutcome(outcome)
	t.ended = time.Now()
	t.outcome = outcome
	if summary != "" {
		t.summary = summary
	}
	g.ended = append(g.ended, t)
	if n := len(g.ended); n > 50 {
		g.ended = append([]*task(nil), g.ended[n-50:]...)
	}
	if outcome != OutcomeStopped {
		// After Stop the gateway leaves the phone alone, reads included.
		g.confirmAtEndLocked(d)
	}
	g.stopVideoLocked(d, outcome != OutcomeStopped)
	g.closeBackgroundLocked(l)
	g.wakeLocked(d)
	g.recordTaskLocked(t)
	leaseID := l.id
	gone := g.approvals.WithdrawWhere(func(i approval.Info) bool { return i.Binding.Lease == leaseID }, outcome)
	for _, info := range gone {
		if ref, ok := g.apSteps[info.ID]; ok {
			delete(g.apSteps, info.ID)
			for i := len(ref.task.steps) - 1; i >= 0; i-- {
				if ref.task.steps[i].Index == ref.index {
					st := ref.task.steps[i]
					st.Status = outcome
					g.updateStepLocked(ref.task, st, "", "")
					break
				}
			}
		}
	}
	g.emit(Event{Kind: EventTasks, DeviceID: d.id, TaskID: t.id})
	g.emit(Event{Kind: EventDevices, DeviceID: d.id})
	g.emit(Event{Kind: EventNeeds, DeviceID: d.id})
}
