// Package companion is the Go client for Pinky's Android companion app
// (android/companion). The app runs an accessibility service that serves
// line-delimited JSON on an abstract socket (@pinky_companion for the
// release build): the window and node tree, ACTION_SET_TEXT input, events
// and settle waits. The client reaches it through `adb forward`, turns the
// service on and off over adb for the length of a task, and converts trees
// to the screen model.
//
// The protocol is described in docs/notes/companion-protocol.md.
package companion

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DIYgod/pinky/internal/screen"
)

// App is one build of the companion app. Releases of Pinky carry the
// release build, signed with Pinky's private release key; builds from the
// source carry a development build, signed with the developer's own key
// under an application id of its own, so that it can never install as an
// update of a released Companion. The two can be installed side by side.
type App struct {
	Package string // the application id
	Socket  string // the abstract socket its service listens on
}

// The companion's builds.
var (
	Release = App{Package: "app.pinky.companion", Socket: "pinky_companion"}
	Dev     = App{Package: "app.pinky.companion.dev", Socket: "pinky_companion_dev"}
)

// ServiceClass is the accessibility service's class, the same in every
// build.
const ServiceClass = "app.pinky.companion.PinkyAccessibilityService"

// Component is the build's service as Android lists it in
// enabled_accessibility_services: "app.pinky.companion/.PinkyAccessibilityService"
// for the release build, the full class name for the development build,
// whose class is outside its package.
func (a App) Component() string {
	if rest, ok := strings.CutPrefix(ServiceClass, a.Package+"."); ok {
		return a.Package + "/." + rest
	}
	return a.Package + "/" + ServiceClass
}

// AppFor returns the build with an application id.
func AppFor(pkg string) (App, bool) {
	for _, a := range []App{Release, Dev} {
		if a.Package == pkg {
			return a, true
		}
	}
	return App{}, false
}

const (
	defaultTimeout = 15 * time.Second
	// DefaultQuiet is how long the screen must stay free of window and
	// content events before WaitSettled calls it settled.
	DefaultQuiet = 300 * time.Millisecond
)

// Client talks to one build of the companion app on one device. Its
// methods are safe for concurrent use; requests on the shared connection
// run one at a time.
type Client struct {
	run Runner
	app App

	mu     sync.Mutex // guards the fields below and serializes requests
	fwd    string     // local address of the adb forward
	conn   net.Conn
	rd     *bufio.Reader
	nextID int64

	// skew is the phone's clock minus the host's, in milliseconds,
	// estimated by Ping.
	skew atomic.Int64

	setMu   sync.Mutex
	restore *restoreState // set while the service is on because Enable turned it on

	subMu sync.Mutex
	subs  map[*Subscription]struct{}
}

// New returns a client for the release build that reaches the device
// through r.
func New(r Runner) *Client { return NewFor(r, Release) }

// NewFor returns a client for one build of the app.
func NewFor(r Runner, app App) *Client {
	return &Client{run: r, app: app, subs: map[*Subscription]struct{}{}}
}

// App returns the build the client talks to.
func (c *Client) App() App { return c.app }

// Close closes the client's connections and its adb forward. It does not
// turn the service off; call Disable for that.
func (c *Client) Close() error {
	c.subMu.Lock()
	subs := c.subs
	c.subs = map[*Subscription]struct{}{}
	c.subMu.Unlock()
	for s := range subs {
		s.Close()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropLocked()
	if c.fwd != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := c.run.RemoveForward(ctx, c.fwd)
		c.fwd = ""
		return err
	}
	return nil
}

// Ping checks that the service answers and returns its version. It also
// measures the phone's clock against the host's, which event times use.
func (c *Client) Ping(ctx context.Context) (*Info, error) {
	var info Info
	sent := time.Now()
	if err := c.call(ctx, "ping", nil, &info, true); err != nil {
		return nil, err
	}
	recv := time.Now()
	mid := sent.Add(recv.Sub(sent) / 2)
	c.skew.Store(info.Time - mid.UnixMilli())
	return &info, nil
}

// DumpOptions tune Dump.
type DumpOptions struct {
	Display   int  // display id; 0 is the default display
	IME       bool // include the keyboard's nodes
	Invisible bool // include nodes that are not visible to the user
	Bars      bool // walk the status and navigation bars too
	MaxNodes  int  // 0 means the app's default (20000)
}

// Dump returns the raw tree of every window on a display.
func (c *Client) Dump(ctx context.Context, opt DumpOptions) (*Tree, error) {
	params := map[string]any{}
	if opt.Display != 0 {
		params["display"] = opt.Display
	}
	if opt.IME {
		params["ime"] = true
	}
	if opt.Invisible {
		params["invisible"] = true
	}
	if opt.Bars {
		params["bars"] = true
	}
	if opt.MaxNodes > 0 {
		params["max_nodes"] = opt.MaxNodes
	}
	var t Tree
	if err := c.call(ctx, "tree", params, &t, true); err != nil {
		return nil, err
	}
	return &t, nil
}

// Tree reads the current screen of the default display in the screen
// model. Refs are left for the gateway to assign.
func (c *Client) Tree(ctx context.Context) (*screen.Screen, error) {
	t, err := c.Dump(ctx, DumpOptions{})
	if err != nil {
		return nil, err
	}
	s := t.Screen(ScreenOptions{})
	s.CapturedAt = c.HostTime(t.Time)
	return s, nil
}

// Target selects the editable node SetText writes into. The zero Target is
// the input that has focus.
type Target struct {
	// Node and Window name a node from a tree response (Node.Handle and
	// Window.ID). Handles stay valid while the view lives.
	Node   int64
	Window int
	// Point picks the smallest editable node under it, top-most window
	// first, preferring one whose resource id equals ID.
	Point *screen.Point
	// ID alone picks the first editable node with this resource id.
	ID string
}

func (t Target) params() map[string]any {
	m := map[string]any{}
	switch {
	case t.Node != 0:
		m["node"] = t.Node
		if t.Window != 0 {
			m["window"] = t.Window
		}
	case t.Point != nil:
		m["x"] = t.Point.X
		m["y"] = t.Point.Y
		if t.ID != "" {
			m["id"] = t.ID
		}
	case t.ID != "":
		m["id"] = t.ID
	default:
		m["focused"] = true
	}
	return m
}

// SetTextOptions tune SetText.
type SetTextOptions struct {
	Append bool // keep the field's current text
	// Submit runs the field's IME action afterwards. Android 10 has no way
	// for an accessibility service to do that; SetTextResult.Submitted is
	// then false and the caller should press Enter itself.
	Submit bool
	// Display is the display whose windows the target is looked up in; 0
	// is the main display. Other displays need an app with
	// FeatureDisplays: an older one ignores the parameter.
	Display int
}

// SetText writes text into an editable node with ACTION_SET_TEXT. Any
// script works, and neither the clipboard nor the keyboard is involved.
func (c *Client) SetText(ctx context.Context, t Target, text string, opt SetTextOptions) (*SetTextResult, error) {
	params := map[string]any{"text": text, "target": t.params()}
	if opt.Display != 0 {
		params["display"] = opt.Display
	}
	if opt.Append {
		params["append"] = true
	}
	if opt.Submit {
		params["submit"] = true
	}
	var r SetTextResult
	// Never retried: an append that reached the phone must not run twice.
	if err := c.call(ctx, "set_text", params, &r, false); err != nil {
		return nil, err
	}
	return &r, nil
}

// WaitIdle waits until no window or content event arrived for quiet, or
// until max passed, and reports whether the screen settled.
func (c *Client) WaitIdle(ctx context.Context, quiet, max time.Duration) (bool, error) {
	var r struct {
		Settled bool `json:"settled"`
	}
	params := map[string]any{"quiet_ms": quiet.Milliseconds(), "timeout_ms": max.Milliseconds()}
	ctx, cancel := context.WithTimeout(ctx, max+5*time.Second)
	defer cancel()
	if err := c.call(ctx, "wait_idle", params, &r, false); err != nil {
		return false, err
	}
	return r.Settled, nil
}

// WaitSettled implements driver.Settler with accessibility events.
func (c *Client) WaitSettled(ctx context.Context, max time.Duration) (bool, error) {
	return c.WaitIdle(ctx, DefaultQuiet, max)
}

// SetIncludeNotImportant makes trees include views that are not important
// for accessibility (layout containers), as `uiautomator dump` does. It is
// off by default: those views add nodes but never labels or actions.
func (c *Client) SetIncludeNotImportant(ctx context.Context, on bool) error {
	return c.call(ctx, "config", map[string]any{"include_not_important": on}, nil, true)
}

// SetIdleDisable sets how long the service waits without any connection
// before it turns itself off; 0 keeps it on. The default is a minute
// (three in builds until 2026-11-08).
func (c *Client) SetIdleDisable(ctx context.Context, d time.Duration) error {
	return c.call(ctx, "config", map[string]any{"idle_disable_ms": d.Milliseconds()}, nil, true)
}

// HostTime converts a phone timestamp (Unix milliseconds) to the host's
// clock, using the skew the last Ping measured.
func (c *Client) HostTime(ms int64) time.Time {
	return time.UnixMilli(ms - c.skew.Load())
}

func (c *Client) call(ctx context.Context, cmd string, params map[string]any, out any, retry bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	reused := c.conn != nil
	err := c.callLocked(ctx, cmd, params, out)
	// A kept connection may have died with an earlier service instance;
	// try once on a fresh one.
	if err != nil && retry && reused && errors.Is(err, ErrNotRunning) && ctx.Err() == nil {
		err = c.callLocked(ctx, cmd, params, out)
	}
	return err
}

func (c *Client) callLocked(ctx context.Context, cmd string, params map[string]any, out any) error {
	if c.conn == nil {
		if err := c.connectLocked(ctx); err != nil {
			return err
		}
	}
	c.nextID++
	id := c.nextID
	msg := make(map[string]any, len(params)+2)
	for k, v := range params {
		msg[k] = v
	}
	msg["id"] = id
	msg["cmd"] = cmd
	line, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	conn, rd := c.conn, c.rd
	deadline := time.Now().Add(defaultTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()

	if _, err := conn.Write(line); err != nil {
		c.dropLocked()
		return c.connError(ctx, err)
	}
	for {
		raw, err := rd.ReadBytes('\n')
		if err != nil {
			c.dropLocked()
			return c.connError(ctx, err)
		}
		var resp response
		if err := json.Unmarshal(raw, &resp); err != nil {
			c.dropLocked()
			return fmt.Errorf("companion: bad response to %s: %w", cmd, err)
		}
		if resp.Event != "" {
			continue
		}
		if resp.ID != id {
			// A connection-level error (such as "forbidden") has no id.
			if resp.ID == 0 && resp.Error != nil {
				c.dropLocked()
				return resp.Error
			}
			continue
		}
		if !resp.OK {
			if resp.Error == nil {
				return fmt.Errorf("companion: %s failed without an error", cmd)
			}
			return resp.Error
		}
		if out != nil {
			if err := json.Unmarshal(resp.Result, out); err != nil {
				return fmt.Errorf("companion: bad %s result: %w", cmd, err)
			}
		}
		return nil
	}
}

func (c *Client) connError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("%w: %v", ErrNotRunning, err)
}

func (c *Client) connectLocked(ctx context.Context) error {
	conn, err := c.dialLocked(ctx)
	if err != nil {
		return err
	}
	c.conn = conn
	c.rd = bufio.NewReaderSize(conn, 256<<10)
	return nil
}

// dialLocked opens a new connection through the adb forward, creating the
// forward first if needed. adb accepts the TCP connection even when nothing
// listens on the phone, so a dead service shows up as EOF on first read.
func (c *Client) dialLocked(ctx context.Context) (net.Conn, error) {
	if c.fwd == "" {
		addr, err := c.run.Forward(ctx, "localabstract:"+c.app.Socket)
		if err != nil {
			return nil, err
		}
		c.fwd = addr
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", c.fwd)
	if err != nil {
		// The forward is gone, for example after an adb server restart.
		c.fwd = ""
		return nil, c.connError(ctx, err)
	}
	return conn, nil
}

func (c *Client) dropLocked() {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.rd = nil
	}
}

// dial opens a separate connection, used by event subscriptions.
func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dialLocked(ctx)
}
