//go:build device

// Tests against a real phone. Build the APK first, then run them while
// holding the phone's lock:
//
//	(cd android/companion && ./gradlew assembleDebug)
//	scripts/devlock.sh $ID env PINKY_DEVICE=$ID go test -tags device -v ./internal/driver/android/companion/
//
// They use only Settings (read), the Calculator and the companion's own
// debug page, and restore enabled_accessibility_services at the end.
package companion

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/screen"
)

func TestDeviceCompanion(t *testing.T) {
	id := os.Getenv("PINKY_DEVICE")
	if id == "" {
		t.Skip("set PINKY_DEVICE and run through scripts/devlock.sh")
	}
	apk := os.Getenv("PINKY_COMPANION_APK")
	if apk == "" {
		apk = filepath.Join("..", "..", "..", "..", "android", "companion", "app", "build", "outputs", "apk", "debug", "app-debug.apk")
	}
	if _, err := os.Stat(apk); err != nil {
		t.Skipf("no APK at %s; build it with ./gradlew assembleDebug in android/companion", apk)
	}
	app, _, err := APKVersion(apk)
	if err != nil {
		t.Fatal(err)
	}
	debugInput := app.Package + ":id/debug_input"
	run := &ExecRunner{Serial: id}
	c := NewFor(run, app)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	did, err := c.InstallIfChanged(ctx, apk)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	t.Logf("installed a new APK: %v", did)

	before := setting(t, run)
	wake(t, run)
	start := time.Now()
	if err := c.Enable(ctx); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	t.Logf("service answered %s after Enable", time.Since(start).Round(time.Millisecond))
	defer func() {
		shell(t, run, "input keyevent KEYCODE_HOME")
		if err := c.Disable(context.Background()); err != nil {
			t.Errorf("Disable: %v", err)
		}
		if after := setting(t, run); after != before {
			t.Errorf("%s is %q after the test, was %q", settingKey, after, before)
		}
	}()
	if s := observe(t, c); s.Locked {
		t.Skip("the phone is locked; leaving it alone")
	}

	t.Run("Settings", func(t *testing.T) {
		wake(t, run)
		shell(t, run, "am start -W -a android.settings.SETTINGS")
		settle(t, c)
		s := observe(t, c)
		if s.App.ID != "com.android.settings" {
			t.Fatalf("foreground is %q:\n%s", s.App.ID, s.Render())
		}
		roles := map[string]int{}
		for _, e := range s.Elements {
			roles[e.Role]++
		}
		if len(s.Elements) < 5 || roles[screen.RoleButton] < 3 || roles[screen.RoleList] < 1 {
			t.Fatalf("thin Settings screen:\n%s", s.Render())
		}
		t.Logf("%d elements, e.g. %s", len(s.Elements), s.Elements[len(s.Elements)-1].Line())
	})

	t.Run("Calculator", func(t *testing.T) {
		wake(t, run)
		comp := calculator(t, run)
		shell(t, run, "am start -W -n "+comp)
		settle(t, c)
		s := observe(t, c)
		if !slices.ContainsFunc(s.Elements, func(e screen.Element) bool { return e.Label == "7" && e.Role == screen.RoleButton }) {
			t.Fatalf("no button 7:\n%s", s.Render())
		}
		t.Logf("%d elements in %s", len(s.Elements), s.App.ID)
	})

	t.Run("SetText", func(t *testing.T) {
		wake(t, run)
		shell(t, run, "am start -W -n "+app.Package+"/"+Release.Package+".MainActivity")
		settle(t, c)
		text := "\u4f60\u597d\uff0cPinky \u4e16\u754c \U0001f389"
		r, err := c.SetText(ctx, Target{ID: debugInput}, text, SetTextOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if !r.Verified || r.Text != text {
			t.Fatalf("set_text: %+v", r)
		}
		settle(t, c)
		s := observe(t, c)
		var input screen.Element
		for _, e := range s.Elements {
			if e.ID == debugInput {
				input = e
			}
		}
		if input.Label != text || input.Role != screen.RoleInput {
			t.Fatalf("input after set_text: %+v\n%s", input, s.Render())
		}
		// The page echoes the field through a TextWatcher, in its own process.
		echo := "Text input length 13: " + text
		if !slices.ContainsFunc(s.Elements, func(e screen.Element) bool { return e.Label == echo }) {
			t.Errorf("echo %q missing:\n%s", echo, s.Render())
		}

		// Append through the point target.
		p := input.Bounds.Center()
		r, err = c.SetText(ctx, Target{Point: &p, ID: debugInput}, " +1", SetTextOptions{Append: true})
		if err != nil || r.Text != text+" +1" {
			t.Fatalf("append: %+v, %v", r, err)
		}

		// A node handle from the raw tree.
		tr, err := c.Dump(ctx, DumpOptions{})
		if err != nil {
			t.Fatal(err)
		}
		win, node := findNode(tr, debugInput)
		if node == nil {
			t.Fatal("debug input not in the raw tree")
		}
		r, err = c.SetText(ctx, Target{Node: node.Handle, Window: win}, "abc", SetTextOptions{})
		if err != nil || !r.Verified || r.Node != node.Handle {
			t.Fatalf("by handle: %+v, %v", r, err)
		}

		// Password fields take text but never report it.
		r, err = c.SetText(ctx, Target{ID: app.Package + ":id/debug_password"}, "s3cret-\u5bc6", SetTextOptions{})
		if err != nil || r.Text != "" {
			t.Fatalf("password: %+v, %v", r, err)
		}
		tr, err = c.Dump(ctx, DumpOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, pw := findNode(tr, app.Package+":id/debug_password"); pw == nil || !pw.Password || pw.Text != "" {
			t.Fatalf("password node: %+v", pw)
		}
		if _, err := c.SetText(ctx, Target{Point: &screen.Point{X: 1, Y: 1}}, "x", SetTextOptions{}); !errors.Is(err, driver.ErrNotFound) {
			t.Errorf("set_text at an empty spot: %v", err)
		}
		// Leave the page as it was.
		for _, idName := range []string{debugInput, app.Package + ":id/debug_password"} {
			if _, err := c.SetText(ctx, Target{ID: idName}, "", SetTextOptions{}); err != nil {
				t.Errorf("clear %s: %v", idName, err)
			}
		}
	})

	t.Run("Events", func(t *testing.T) {
		wake(t, run)
		if _, err := c.Ping(ctx); err != nil { // refreshes the clock skew
			t.Fatal(err)
		}
		sctx, scancel := context.WithTimeout(ctx, 15*time.Second)
		defer scancel()
		sub, err := c.Events(sctx, EventToast, EventWindow)
		if err != nil {
			t.Fatal(err)
		}
		defer sub.Close()
		text := "Pinky \u6d4b\u8bd5 \u2713"
		sent := time.Now()
		shell(t, run, "am start -n "+app.Package+"/"+Release.Package+".MainActivity --es toast "+shellQuote(text))
		for ev := range sub.C() {
			if ev.Kind == EventToast && ev.Text == text {
				if d := ev.Time.Sub(sent); d < -2*time.Second || d > 5*time.Second {
					t.Errorf("toast time is %s after the request", d)
				}
				return
			}
		}
		t.Fatalf("no toast event: %v", sub.Err())
	})
}

func observe(t *testing.T, c *Client) *screen.Screen {
	t.Helper()
	s, err := c.Tree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	screen.NewRefs().Assign(s)
	return s
}

func settle(t *testing.T, c *Client) {
	t.Helper()
	if _, err := c.WaitSettled(context.Background(), 3*time.Second); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
}

func shell(t *testing.T, run Runner, cmd string) string {
	t.Helper()
	out, err := run.Shell(context.Background(), cmd)
	if err != nil {
		t.Fatalf("%s: %v", cmd, err)
	}
	return out
}

func setting(t *testing.T, run Runner) string {
	return strings.TrimSpace(shell(t, run, "settings get secure "+settingKey))
}

// wake turns a screen that timed out back on. KEYCODE_WAKEUP is not the
// power key and does nothing to a screen that is on.
func wake(t *testing.T, run Runner) {
	t.Helper()
	if !strings.Contains(shell(t, run, "dumpsys power | grep -m1 mWakefulness="), "Awake") {
		shell(t, run, "input keyevent KEYCODE_WAKEUP")
		time.Sleep(time.Second)
	}
}

// calculator returns the launcher component of the phone's calculator.
func calculator(t *testing.T, run Runner) string {
	t.Helper()
	for _, pkg := range []string{"com.google.android.calculator", "com.android.calculator2", "org.lineageos.calculator", "com.miui.calculator", "com.sec.android.app.popupcalculator"} {
		out, err := run.Shell(context.Background(), "cmd package resolve-activity --brief -c android.intent.category.LAUNCHER "+pkg)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if last := strings.TrimSpace(lines[len(lines)-1]); strings.HasPrefix(last, pkg+"/") {
			return last
		}
	}
	t.Skip("no known calculator on this phone")
	return ""
}

func findNode(tr *Tree, id string) (int, *Node) {
	var walk func(*Node) *Node
	walk = func(n *Node) *Node {
		if n == nil {
			return nil
		}
		if n.ID == id {
			return n
		}
		for _, c := range n.Children {
			if f := walk(c); f != nil {
				return f
			}
		}
		return nil
	}
	for _, w := range tr.Windows {
		if n := walk(w.Root); n != nil {
			return w.ID, n
		}
	}
	return 0, nil
}
