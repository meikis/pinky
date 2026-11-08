// Command companionctl drives the Android companion app by hand: install
// it, turn its accessibility service on and off, and read trees, set text
// and watch events. Run it through scripts/devlock.sh on a shared phone.
//
//	go run ./internal/driver/android/companion/cmd/companionctl -s SERIAL <command> [args]
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DIYgod/pinky/internal/driver/android/companion"
	"github.com/DIYgod/pinky/internal/screen"
)

const usage = `usage: companionctl -s SERIAL [-dev] [-keep] <command> [args]

commands:
  install [-force] APK adb install -r the companion APK, unless the phone
                       already has this exact file
  installed            print the installed version
  enable               turn the accessibility service on and leave it on
  disable              remove Pinky's entry from the setting
  status               show whether the service is listed and answering
  ping                 print the ping result
  tree [-raw] [-ime]   print the screen (or the raw JSON tree)
  settext [-append] [-submit] [-id ID] [-x X -y Y] TEXT
                       write TEXT into the focused (or chosen) field
  events [KIND...]     print events until interrupted
  idle [MAX_MS]        wait for the screen to settle
  bench [N]            time N companion trees against N uiautomator dumps
  coldstart [-cold] [N]
                       time turning the service on to the first tree, N
                       times; -cold stops the app's process before each
  signer APK|-         print the name Android gives an APK's signer, or
                       a DER certificate's on stdin, as dumpsys package
                       shows it (signatures:[...]); needs no phone

Commands from ping on turn the service on for their run and restore
enabled_accessibility_services afterwards; -keep leaves it on. "enable"
leaves it on; "disable" removes Pinky's entry whoever added it;
"coldstart" leaves the service as it found it. -dev talks to the
development build (app.pinky.companion.dev, what make companion builds)
instead of the release build.`

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "signer" {
		if err := signer(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "companionctl:", err)
			os.Exit(1)
		}
		return
	}
	serial := flag.String("s", os.Getenv("PINKY_DEVICE"), "device serial (default $PINKY_DEVICE)")
	keep := flag.Bool("keep", false, "leave the service on after the command")
	dev := flag.Bool("dev", false, "use the development build, app.pinky.companion.dev")
	flag.Usage = func() { fmt.Fprintln(os.Stderr, usage) }
	flag.Parse()
	if *serial == "" || flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	run := &companion.ExecRunner{Serial: *serial}
	app := companion.Release
	if *dev {
		app = companion.Dev
	}
	c := companion.NewFor(run, app)
	defer c.Close()
	if err := dispatch(ctx, c, run, flag.Arg(0), flag.Args()[1:], *keep); err != nil {
		fmt.Fprintln(os.Stderr, "companionctl:", err)
		os.Exit(1)
	}
}

func dispatch(ctx context.Context, c *companion.Client, run *companion.ExecRunner, cmd string, args []string, keep bool) error {
	switch cmd {
	case "install":
		fs := flag.NewFlagSet("install", flag.ContinueOnError)
		force := fs.Bool("force", false, "install even if the phone has this exact APK")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New("install needs the APK path")
		}
		if *force {
			return c.Install(ctx, fs.Arg(0))
		}
		did, err := c.InstallIfChanged(ctx, fs.Arg(0))
		if err == nil && !did {
			fmt.Println("already installed")
		}
		return err
	case "installed":
		v, ok, err := c.Installed(ctx)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("not installed")
			return nil
		}
		fmt.Println(v)
		return nil
	case "enable":
		// Leaves the service on; "disable" turns it off again.
		return c.Enable(ctx)
	case "disable":
		return c.ForceDisable(ctx)
	case "status":
		on, err := c.Enabled(ctx)
		if err != nil {
			return err
		}
		fmt.Println("listed:", on)
		info, err := c.Ping(ctx)
		if err != nil {
			fmt.Println("answering: no:", err)
			return nil
		}
		fmt.Printf("answering: yes, version %s, Android %s (SDK %d), %s %s\n",
			info.Version, info.Release, info.SDK, info.Manufacturer, info.Model)
		return nil
	}

	if cmd == "coldstart" {
		return coldstart(ctx, c, run, args)
	}
	switch cmd {
	case "ping", "tree", "settext", "events", "idle", "bench":
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
	// The rest need the service: turn it on for the command and restore the
	// setting afterwards, unless -keep.
	if err := c.Enable(ctx); err != nil {
		return err
	}
	if !keep {
		defer func() {
			dctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := c.Disable(dctx); err != nil {
				fmt.Fprintln(os.Stderr, "companionctl: disable:", err)
			}
		}()
	}
	switch cmd {
	case "ping":
		info, err := c.Ping(ctx)
		if err != nil {
			return err
		}
		return printJSON(info)
	case "tree":
		return tree(ctx, c, args)
	case "settext":
		return setText(ctx, c, args)
	case "events":
		return events(ctx, c, args)
	case "idle":
		max := 3 * time.Second
		if len(args) > 0 {
			ms, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			max = time.Duration(ms) * time.Millisecond
		}
		start := time.Now()
		settled, err := c.WaitSettled(ctx, max)
		if err != nil {
			return err
		}
		fmt.Printf("settled=%v after %s\n", settled, time.Since(start).Round(time.Millisecond))
		return nil
	case "bench":
		n := 10
		if len(args) > 0 {
			v, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			n = v
		}
		return bench(ctx, c, run, n)
	}
	return fmt.Errorf("unknown command %q", cmd)
}

func tree(ctx context.Context, c *companion.Client, args []string) error {
	fs := flag.NewFlagSet("tree", flag.ContinueOnError)
	raw := fs.Bool("raw", false, "print the raw JSON tree")
	ime := fs.Bool("ime", false, "include the keyboard")
	bars := fs.Bool("bars", false, "include the status and navigation bars")
	if err := fs.Parse(args); err != nil {
		return err
	}
	start := time.Now()
	t, err := c.Dump(ctx, companion.DumpOptions{IME: *ime, Bars: *bars})
	if err != nil {
		return err
	}
	took := time.Since(start)
	if *raw {
		return printJSON(t)
	}
	s := t.Screen(companion.ScreenOptions{Keyboard: *ime, SystemBars: *bars})
	screen.NewRefs().Assign(s)
	fmt.Println(s.Render())
	fmt.Fprintf(os.Stderr, "%d nodes, %d elements, phone %d ms, round trip %s\n",
		t.Nodes, len(s.Elements), t.TookMS, took.Round(time.Millisecond))
	return nil
}

func setText(ctx context.Context, c *companion.Client, args []string) error {
	fs := flag.NewFlagSet("settext", flag.ContinueOnError)
	appendText := fs.Bool("append", false, "keep the current text")
	submit := fs.Bool("submit", false, "run the IME action afterwards")
	id := fs.String("id", "", "resource id of the field")
	x := fs.Int("x", -1, "x of a point inside the field")
	y := fs.Int("y", -1, "y of a point inside the field")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("settext needs exactly one TEXT argument")
	}
	var t companion.Target
	if *x >= 0 && *y >= 0 {
		t.Point = &screen.Point{X: *x, Y: *y}
	}
	t.ID = *id
	r, err := c.SetText(ctx, t, fs.Arg(0), companion.SetTextOptions{Append: *appendText, Submit: *submit})
	if err != nil {
		return err
	}
	return printJSON(r)
}

func events(ctx context.Context, c *companion.Client, args []string) error {
	if _, err := c.Ping(ctx); err != nil { // measures the clock skew
		return err
	}
	var kinds []companion.EventKind
	for _, a := range args {
		kinds = append(kinds, companion.EventKind(a))
	}
	sub, err := c.Events(ctx, kinds...)
	if err != nil {
		return err
	}
	for ev := range sub.C() {
		b, _ := json.Marshal(ev)
		fmt.Printf("%s %s\n", ev.Time.Format("15:04:05.000"), b)
	}
	return sub.Err()
}

// bench times companion trees, then uiautomator dumps, of the screen in
// front. Dumps come last because each one makes Android unbind every other
// accessibility service while it runs. Samples taken while another app (or
// the lock screen after a screen timeout) was in front are discarded, and a
// screen that went dark is woken with KEYCODE_WAKEUP; nothing else is
// touched.
func bench(ctx context.Context, c *companion.Client, run *companion.ExecRunner, n int) error {
	var tree, phone, dump []time.Duration
	var nodes, elements, dumpNodes int
	target := ""
	for tries := 0; len(tree) < n; tries++ {
		if tries > 3*n {
			return errors.New("bench: the screen kept changing")
		}
		if err := ensureAwake(ctx, run); err != nil {
			return err
		}
		start := time.Now()
		t, err := c.Dump(ctx, companion.DumpOptions{})
		if err != nil {
			return err
		}
		d := time.Since(start)
		s := t.Screen(companion.ScreenOptions{})
		if s.Locked {
			return errors.New("bench: the phone is locked; leaving it alone")
		}
		if target == "" {
			target = s.App.ID
		}
		if s.App.ID != target {
			continue
		}
		tree = append(tree, d)
		phone = append(phone, time.Duration(t.TookMS)*time.Millisecond)
		nodes, elements = t.Nodes, len(s.Elements)
		time.Sleep(300 * time.Millisecond)
	}
	for tries := 0; len(dump) < n; tries++ {
		if tries > 3*n {
			return errors.New("bench: the screen kept changing")
		}
		if err := ensureAwake(ctx, run); err != nil {
			return err
		}
		start := time.Now()
		out, err := uiautomatorDump(ctx, run)
		if err != nil {
			return err
		}
		d := time.Since(start)
		if !strings.Contains(out, `package="`+target+`"`) {
			continue
		}
		dump = append(dump, d)
		dumpNodes = strings.Count(out, "<node ")
		time.Sleep(300 * time.Millisecond)
	}
	fmt.Printf("app: %s\n", target)
	fmt.Printf("companion tree:    %s; phone side %s; %d nodes, %d elements\n", stats(tree), stats(phone), nodes, elements)
	fmt.Printf("uiautomator dump:  %s; %d nodes\n", stats(dump), dumpNodes)
	return nil
}

// coldstart times the first read of a task now that Pinky turns the
// service on only while an agent works: writing the setting until the
// service answers, then the first tree and the event subscription the
// driver opens, against a tree once the service is on. With -cold the
// app's process is stopped first, as Android does to an app left unused
// for long. It needs an unlocked phone whose screen is on, touches
// nothing, and leaves the service as it found it.
func coldstart(ctx context.Context, c *companion.Client, run *companion.ExecRunner, args []string) error {
	fs := flag.NewFlagSet("coldstart", flag.ContinueOnError)
	cold := fs.Bool("cold", false, "stop the app's process before each round")
	if err := fs.Parse(args); err != nil {
		return err
	}
	n := 5
	if fs.NArg() > 0 {
		v, err := strconv.Atoi(fs.Arg(0))
		if err != nil {
			return err
		}
		n = v
	}
	wasOn, err := c.Enabled(ctx)
	if err != nil {
		return err
	}
	var idle int64 = -1
	if wasOn {
		if info, err := c.Ping(ctx); err == nil {
			idle = info.IdleDisableMS
		}
	}
	defer func() {
		rctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var err error
		switch {
		case wasOn:
			// TurnOn keeps it on (idle_disable_ms 0); put back the limit
			// it had.
			if err = c.TurnOn(rctx); err == nil && idle > 0 {
				err = c.SetIdleDisable(rctx, time.Duration(idle)*time.Millisecond)
			}
		default:
			err = c.ForceDisable(rctx)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "companionctl: restoring the service:", err)
		}
	}()
	var enable, first, total, sub, warm []time.Duration
	nodes := 0
	for i := 0; i < n; i++ {
		if err := offAndGone(ctx, c); err != nil {
			return err
		}
		if *cold {
			if _, err := run.Shell(ctx, "am force-stop "+c.App().Package); err != nil {
				return err
			}
		}
		time.Sleep(time.Second)
		if err := awakeUnlocked(ctx, run); err != nil {
			return err
		}
		t0 := time.Now()
		if err := c.Enable(ctx); err != nil {
			return err
		}
		t1 := time.Now()
		t, err := c.Dump(ctx, companion.DumpOptions{})
		if err != nil {
			return err
		}
		t2 := time.Now()
		s, err := c.Events(ctx, companion.EventWindow, companion.EventWindows, companion.EventContent,
			companion.EventToast, companion.EventNotification)
		if err != nil {
			return err
		}
		t3 := time.Now()
		if _, err := c.Dump(ctx, companion.DumpOptions{}); err != nil {
			s.Close()
			return err
		}
		t4 := time.Now()
		s.Close()
		enable = append(enable, t1.Sub(t0))
		first = append(first, t2.Sub(t1))
		total = append(total, t2.Sub(t0))
		sub = append(sub, t3.Sub(t2))
		warm = append(warm, t4.Sub(t3))
		nodes = t.Nodes
		fmt.Fprintf(os.Stderr, "round %d: on in %s, first tree %s (%d nodes, phone %d ms), subscribe %s, next tree %s\n",
			i+1, t1.Sub(t0).Round(time.Millisecond), t2.Sub(t1).Round(time.Millisecond), t.Nodes, t.TookMS,
			t3.Sub(t2).Round(time.Millisecond), t4.Sub(t3).Round(time.Millisecond))
	}
	fmt.Printf("setting written to answering: %s\n", stats(enable))
	fmt.Printf("then the first tree:          %s (%d nodes)\n", stats(first), nodes)
	fmt.Printf("off to the first tree:        %s\n", stats(total))
	fmt.Printf("event subscription:           %s\n", stats(sub))
	fmt.Printf("a tree once on:               %s\n", stats(warm))
	return nil
}

// signer prints the signer name of an APK, or of a DER certificate on
// stdin.
func signer(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: companionctl signer APK|-")
	}
	var cert []byte
	var err error
	if args[0] == "-" {
		cert, err = io.ReadAll(os.Stdin)
		if err == nil && len(cert) == 0 {
			err = errors.New("no certificate on stdin")
		}
	} else {
		cert, err = companion.APKCertificate(args[0])
	}
	if err != nil {
		return err
	}
	fmt.Println(companion.SignerName(cert))
	return nil
}

// offAndGone turns the service off and waits until it no longer answers.
func offAndGone(ctx context.Context, c *companion.Client) error {
	if err := c.ForceDisable(ctx); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		pctx, cancel := context.WithTimeout(ctx, time.Second)
		_, err := c.Ping(pctx)
		cancel()
		if errors.Is(err, companion.ErrNotRunning) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("coldstart: the service still answers after it was turned off")
}

// awakeUnlocked refuses a phone whose screen is off or locked: coldstart
// presses nothing.
func awakeUnlocked(ctx context.Context, run *companion.ExecRunner) error {
	out, err := run.Shell(ctx, "dumpsys power | grep -m1 mWakefulness=; dumpsys window policy | grep -E 'showing=|mIsShowing=|isKeyguardShowing='")
	if err != nil {
		return err
	}
	if !strings.Contains(out, "Awake") {
		return errors.New("coldstart: the screen is off; leaving the phone alone")
	}
	if strings.Contains(out, "showing=true") || strings.Contains(out, "mIsShowing=true") || strings.Contains(out, "isKeyguardShowing=true") {
		return errors.New("coldstart: the phone is locked; leaving it alone")
	}
	return nil
}

// ensureAwake wakes a screen that timed out. KEYCODE_WAKEUP does nothing
// on a screen that is already on.
func ensureAwake(ctx context.Context, run *companion.ExecRunner) error {
	out, err := run.Shell(ctx, "dumpsys power | grep -m1 mWakefulness=")
	if err != nil {
		return err
	}
	if strings.Contains(out, "Awake") {
		return nil
	}
	if _, err := run.Shell(ctx, "input keyevent KEYCODE_WAKEUP"); err != nil {
		return err
	}
	time.Sleep(1500 * time.Millisecond)
	return nil
}

// uiautomatorDump runs `uiautomator dump` and reads the XML back, the way a
// driver without the companion has to.
func uiautomatorDump(ctx context.Context, run *companion.ExecRunner) (string, error) {
	const path = "/data/local/tmp/pinky-window-dump.xml"
	out, err := run.Shell(ctx, "uiautomator dump "+path+" >/dev/null && cat "+path+" && rm "+path)
	if err != nil {
		return "", err
	}
	if !strings.Contains(out, "<hierarchy") {
		return "", fmt.Errorf("uiautomator dump: %s", strings.TrimSpace(out))
	}
	return out, nil
}

func stats(ds []time.Duration) string {
	if len(ds) == 0 {
		return "-"
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	var sum time.Duration
	for _, d := range s {
		sum += d
	}
	ms := func(d time.Duration) string { return strconv.FormatFloat(float64(d)/1e6, 'f', 0, 64) }
	return fmt.Sprintf("median %s ms, mean %s, min %s, max %s (n=%d)",
		ms(s[len(s)/2]), ms(sum/time.Duration(len(s))), ms(s[0]), ms(s[len(s)-1]), len(s))
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
