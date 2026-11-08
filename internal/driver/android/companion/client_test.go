package companion

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/screen"
)

func readyClient(t *testing.T) (*fakePhone, *Client) {
	t.Helper()
	p := newFakePhone(t)
	c := enableDisable(t, p)
	return p, c
}

func TestPingMeasuresClockSkew(t *testing.T) {
	p, c := readyClient(t)
	p.mu.Lock()
	p.skew = 90 * time.Second
	p.mu.Unlock()
	info, err := c.Ping(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Protocol != ProtocolVersion || info.Version != "0.1.0" {
		t.Errorf("info: %+v", info)
	}
	// A phone timestamp taken now maps back to about now on the host.
	got := c.HostTime(time.Now().Add(90 * time.Second).UnixMilli())
	if d := time.Since(got); d < -time.Second || d > time.Second {
		t.Errorf("HostTime is off by %s", d)
	}
}

func TestTreeAndScreen(t *testing.T) {
	_, c := readyClient(t)
	s, err := c.Tree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.App.ID != Release.Package || len(s.Elements) != 2 || s.Width != 1080 {
		t.Fatalf("screen: %s", s.Render())
	}
	if s.CapturedAt.IsZero() {
		t.Error("CapturedAt not set")
	}
	raw, err := c.Dump(context.Background(), DumpOptions{IME: true, Bars: true, MaxNodes: 500})
	if err != nil {
		t.Fatal(err)
	}
	if raw.Nodes != 3 {
		t.Errorf("nodes = %d", raw.Nodes)
	}
}

func TestDumpSendsOptions(t *testing.T) {
	p, c := readyClient(t)
	if _, err := c.Dump(context.Background(), DumpOptions{Display: 2, IME: true, Invisible: true, Bars: true, MaxNodes: 50}); err != nil {
		t.Fatal(err)
	}
	req := p.lastRequest("tree")
	for k, want := range map[string]any{"display": 2.0, "ime": true, "invisible": true, "bars": true, "max_nodes": 50.0} {
		if req[k] != want {
			t.Errorf("%s = %v, want %v", k, req[k], want)
		}
	}
}

func TestSetTextTargets(t *testing.T) {
	p, c := readyClient(t)
	ctx := context.Background()
	text := "你好，Pinky \U0001f389"
	r, err := c.SetText(ctx, Target{}, text, SetTextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != text || !r.Verified || r.Node != 4242 {
		t.Errorf("result: %+v", r)
	}
	req := p.lastRequest("set_text")
	if req["text"] != text || req["target"].(map[string]any)["focused"] != true {
		t.Errorf("request: %v", req)
	}

	cases := []struct {
		target Target
		want   map[string]any
	}{
		{Target{Node: 4242, Window: 41}, map[string]any{"node": 4242.0, "window": 41.0}},
		{Target{Point: &screen.Point{X: 540, Y: 1296}, ID: "app:id/in"}, map[string]any{"x": 540.0, "y": 1296.0, "id": "app:id/in"}},
		{Target{ID: "app:id/in"}, map[string]any{"id": "app:id/in"}},
	}
	for _, tc := range cases {
		if _, err := c.SetText(ctx, tc.target, "x", SetTextOptions{Append: true, Submit: true}); err != nil {
			t.Fatal(err)
		}
		req := p.lastRequest("set_text")
		got := req["target"].(map[string]any)
		if len(got) != len(tc.want) {
			t.Errorf("target %+v sent %v", tc.target, got)
		}
		for k, v := range tc.want {
			if got[k] != v {
				t.Errorf("target %+v: %s = %v, want %v", tc.target, k, got[k], v)
			}
		}
		if req["append"] != true || req["submit"] != true {
			t.Errorf("options not sent: %v", req)
		}
	}
}

func TestErrorsMapToDriverErrors(t *testing.T) {
	p, c := readyClient(t)
	p.setText = func(map[string]any) (any, *Error) {
		return nil, &Error{Code: "not_found", Message: "no editable node at 1,2"}
	}
	_, err := c.SetText(context.Background(), Target{Point: &screen.Point{X: 1, Y: 2}}, "x", SetTextOptions{})
	var cerr *Error
	if !errors.As(err, &cerr) || cerr.Code != "not_found" || !errors.Is(err, driver.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	p.setText = func(map[string]any) (any, *Error) {
		return nil, &Error{Code: "unsupported", Message: "cannot append to a password field"}
	}
	if _, err := c.SetText(context.Background(), Target{}, "x", SetTextOptions{Append: true}); !errors.Is(err, driver.ErrUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestReconnectsAfterServiceRestart(t *testing.T) {
	p, c := readyClient(t)
	ctx := context.Background()
	p.mu.Lock()
	p.dropAfter = 1 // each connection serves one request, like a restarting service
	p.mu.Unlock()
	for i := 0; i < 3; i++ {
		if _, err := c.Ping(ctx); err != nil {
			t.Fatalf("ping %d: %v", i, err)
		}
	}
}

func TestNotRunning(t *testing.T) {
	p := newFakePhone(t)
	c := New(p)
	defer c.Close()
	_, err := c.Ping(context.Background())
	if !errors.Is(err, ErrNotRunning) {
		t.Fatalf("err = %v, want ErrNotRunning", err)
	}
}

func TestSetTextIsNotRetried(t *testing.T) {
	p, c := readyClient(t)
	p.mu.Lock()
	p.dropAfter = 1
	p.mu.Unlock()
	ctx := context.Background()
	if _, err := c.Ping(ctx); err != nil { // uses up the kept connection
		t.Fatal(err)
	}
	p.mu.Lock()
	n := len(p.requests)
	p.mu.Unlock()
	// The kept connection is dead; set_text must fail rather than resend.
	if _, err := c.SetText(ctx, Target{}, "x", SetTextOptions{Append: true}); err == nil {
		// The first attempt may land on a fresh connection; then it ran once.
		p.mu.Lock()
		sent := 0
		for _, r := range p.requests[n:] {
			if r["cmd"] == "set_text" {
				sent++
			}
		}
		p.mu.Unlock()
		if sent != 1 {
			t.Fatalf("set_text sent %d times", sent)
		}
	}
}

func TestEvents(t *testing.T) {
	p, c := readyClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := c.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	sub, err := c.Events(ctx, EventToast, EventNotification)
	if err != nil {
		t.Fatal(err)
	}
	if req := p.lastRequest("events"); req["kinds"].([]any)[0] != "toast" {
		t.Errorf("events request: %v", req)
	}
	now := time.Now().UnixMilli()
	p.events <- `{"event":"toast","time":` + itoa(now) + `,"package":"app.pinky.companion","text":"Toast 测试"}`
	p.events <- `{"event":"notification","time":` + itoa(now) + `,"display":-1,"package":"com.example","title":"Alice","text":"See you at 7","category":"msg"}`
	p.events <- `{"event":"content","time":` + itoa(now) + `,"display":7,"package":"com.example","count":3}`

	var got []Event
	for len(got) < 3 {
		select {
		case ev, ok := <-sub.C():
			if !ok {
				t.Fatalf("closed early: %v", sub.Err())
			}
			got = append(got, ev)
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for events")
		}
	}
	if got[0].Kind != EventToast || got[0].Text != "Toast 测试" {
		t.Errorf("toast: %+v", got[0])
	}
	if d := time.Since(got[0].Time); d < -time.Second || d > time.Second {
		t.Errorf("event time off by %s", d)
	}
	se, ok := got[1].ScreenEvent()
	if !ok || se.Line() != `notification: com.example · "Alice: See you at 7"` {
		t.Errorf("notification line: %q", se.Line())
	}
	if se, ok := got[0].ScreenEvent(); !ok || se.Line() != `toast: "Toast `+"测试"+`"` {
		t.Errorf("toast line: %q", se.Line())
	}
	if _, ok := got[2].ScreenEvent(); ok || got[2].Count != 3 {
		t.Errorf("content event: %+v", got[2])
	}
	// The display: as sent, and -1 (not known) when left out, as apps
	// from before round three do below Android 13; never 0.
	if got[0].Display != -1 || got[1].Display != -1 || got[2].Display != 7 {
		t.Errorf("displays: %d, %d, %d; want -1, -1, 7", got[0].Display, got[1].Display, got[2].Display)
	}

	// The service going away ends the subscription with ErrNotRunning.
	close(p.events)
	select {
	case _, ok := <-sub.C():
		if ok {
			t.Fatal("unexpected event")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("subscription did not end")
	}
	if !errors.Is(sub.Err(), ErrNotRunning) {
		t.Errorf("Err = %v", sub.Err())
	}
}

func TestEventsCloseIsQuiet(t *testing.T) {
	_, c := readyClient(t)
	sub, err := c.Events(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sub.Close()
	for range sub.C() {
	}
	if sub.Err() != nil {
		t.Errorf("Err after Close = %v", sub.Err())
	}
}

func TestWaitSettledAndConfig(t *testing.T) {
	p, c := readyClient(t)
	ctx := context.Background()
	settled, err := c.WaitSettled(ctx, 2*time.Second)
	if err != nil || !settled {
		t.Fatalf("WaitSettled = %v, %v", settled, err)
	}
	if req := p.lastRequest("wait_idle"); req["quiet_ms"] != 300.0 || req["timeout_ms"] != 2000.0 {
		t.Errorf("wait_idle request: %v", req)
	}
	if err := c.SetIncludeNotImportant(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := c.SetIdleDisable(ctx, time.Minute); err != nil {
		t.Fatal(err)
	}
	if req := p.lastRequest("config"); req["idle_disable_ms"] != 60000.0 {
		t.Errorf("config request: %v", req)
	}
}

func TestInstall(t *testing.T) {
	p := newFakePhone(t)
	c := New(p)
	defer c.Close()
	ctx := context.Background()
	if _, ok, err := c.Installed(ctx); err != nil || ok {
		t.Fatalf("before install: ok=%v err=%v", ok, err)
	}
	dir := t.TempDir()
	apk := filepath.Join(dir, "app-debug.apk")
	if err := os.WriteFile(apk, []byte("apk v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i, want := range []bool{true, false} {
		did, err := c.InstallIfChanged(ctx, apk)
		if err != nil || did != want {
			t.Fatalf("InstallIfChanged #%d = %v, %v; want %v", i, did, err, want)
		}
	}
	if err := os.WriteFile(apk, []byte("apk v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if did, err := c.InstallIfChanged(ctx, apk); err != nil || !did {
		t.Fatalf("changed APK: %v, %v", did, err)
	}
	if p.installs != 2 {
		t.Errorf("installs = %d, want 2", p.installs)
	}
	v, ok, err := c.Installed(ctx)
	if err != nil || !ok || v != "0.1.0" {
		t.Fatalf("Installed = %q, %v, %v", v, ok, err)
	}
}

var _ driver.Settler = (*Client)(nil)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
