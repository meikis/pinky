package fakeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/gateway"
	"github.com/DIYgod/pinky/internal/video"
)

// The gateway's round-three extras: pairing over Wi-Fi, the Android
// companion app, background displays, screen recordings, retention and
// export. Nothing here touches a real phone: pairing adds a fake Pixel 8a,
// recordings are drawn, and the agent's display is an ffmpeg test source.

// features says which optional controls a fake phone offers. Like the
// gateway's, they are all off while it is offline.
func (a *API) features(d *device) gateway.Features {
	if d.info.State != driver.StateOnline {
		return gateway.Features{}
	}
	android := d.info.Platform == driver.Android
	return gateway.Features{Unlock: true, Background: android, Files: android, Companion: android}
}

// --- pairing ---

// pairDelay is how long a QR pairing waits for its fake phone to scan the
// code, at Speed 1.
const pairDelay = 6 * time.Second

type pairing struct {
	a       *API
	text    string
	scanned chan struct{}
	closed  chan struct{}
	once    sync.Once
}

// StartPairing begins a QR pairing. A fake phone "scans" the code after a
// few seconds (Options.PairDelay), or when ScanPairing says so.
func (a *API) StartPairing(ctx context.Context) (driver.Pairing, error) {
	a.mu.Lock()
	a.nextID++
	p := &pairing{a: a, text: fmt.Sprintf("WIFI:T:ADB;S:pinky-%04d;P:%06d;;", a.nextID, 314159+a.nextID),
		scanned: make(chan struct{}), closed: make(chan struct{})}
	a.pairings = append(a.pairings, p)
	delay := a.opt.PairDelay
	a.mu.Unlock()
	if delay == 0 {
		delay = time.Duration(float64(pairDelay) / a.opt.Speed)
	}
	if delay > 0 {
		time.AfterFunc(delay, p.scan)
	}
	return p, nil
}

// ScanPairing has the fake phone scan the open QR codes now.
func (a *API) ScanPairing() {
	a.mu.Lock()
	ps := a.pairings
	a.pairings = nil
	a.mu.Unlock()
	for _, p := range ps {
		p.scan()
	}
}

func (p *pairing) scan() {
	select {
	case <-p.scanned:
	default:
		close(p.scanned)
	}
}

func (p *pairing) QR() string { return p.text }

func (p *pairing) Wait(ctx context.Context) (driver.Info, error) {
	select {
	case <-ctx.Done():
		return driver.Info{}, ctx.Err()
	case <-p.closed:
		return driver.Info{}, errors.New("fakeapi: the pairing was closed")
	case <-p.scanned:
		return p.a.addWiFiPhone(), nil
	}
}

func (p *pairing) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

// Progress implements driver.PairingReporter: what SetPairingProgress
// set.
func (p *pairing) Progress() driver.PairingProgress {
	p.a.mu.Lock()
	defer p.a.mu.Unlock()
	return p.a.pairProg
}

// SetPairingProgress sets what the QR pairings report, as a real phone
// would move them on: found on the network, paired, or a search the
// system blocks.
func (a *API) SetPairingProgress(pr driver.PairingProgress) {
	a.mu.Lock()
	a.pairProg = pr
	a.mu.Unlock()
}

// PairWithCode pairs a fake phone by pairing code. The code 000000 is
// wrong, port 1 refuses, and a phone at port 2 pairs but does not show up
// on the network (ConnectWiFi then connects it), to show the errors.
func (a *API) PairWithCode(ctx context.Context, addr, code string) (driver.Info, error) {
	select {
	case <-ctx.Done():
		return driver.Info{}, ctx.Err()
	case <-time.After(time.Duration(float64(1500*time.Millisecond) / a.opt.Speed)):
	}
	switch {
	case code == "000000":
		return driver.Info{}, errors.New("adb: failed to pair: Wrong password or connection was dropped")
	case strings.HasSuffix(addr, ":1"):
		return driver.Info{}, fmt.Errorf("adb: dial tcp %s: connect: connection refused", addr)
	case strings.HasSuffix(addr, ":2"):
		return driver.Info{}, fmt.Errorf("android: the phone paired, but its wireless debugging service did not show up: %w", driver.ErrPairedNotFound)
	}
	return a.addWiFiPhone(), nil
}

// ConnectWiFi connects a fake phone that paired at the address it shows.
// Port 1 refuses.
func (a *API) ConnectWiFi(ctx context.Context, addr string) (driver.Info, error) {
	select {
	case <-ctx.Done():
		return driver.Info{}, ctx.Err()
	case <-time.After(time.Duration(float64(800*time.Millisecond) / a.opt.Speed)):
	}
	if strings.HasSuffix(addr, ":1") {
		return driver.Info{}, fmt.Errorf("adb: connect %s: failed to connect to '%s': Connection refused", addr, addr)
	}
	return a.addWiFiPhone(), nil
}

// addWiFiPhone adds a phone paired over Wi-Fi.
func (a *API) addWiFiPhone() driver.Info {
	a.mu.Lock()
	a.nextID++
	d := &device{
		info: driver.Info{ID: fmt.Sprintf("pixel-8a-wifi-%d", a.nextID), Platform: driver.Android, Name: "Pixel 8a",
			Model: "Pixel 8a", OSVersion: "16", Connection: "wifi", Backend: "scrcpy", State: driver.StateOnline},
		width: 1080, height: 2400, codec: driver.H264, source: "rgbtestsrc", companion: driver.CompanionMissing,
	}
	a.devices = append(a.devices, d)
	info := d.info
	a.mu.Unlock()
	a.emit(gateway.Event{Kind: gateway.EventDevices, DeviceID: info.ID})
	return info
}

// --- the companion app ---

// CompanionState reports a fake phone's companion app.
func (a *API) CompanionState(ctx context.Context, deviceID string) (driver.CompanionState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	d := a.device(deviceID)
	switch {
	case d == nil:
		return "", fmt.Errorf("fakeapi: no device %q: %w", deviceID, driver.ErrNotFound)
	case d.info.Platform != driver.Android:
		return "", fmt.Errorf("%s has no companion app: %w", d.info.Name, driver.ErrUnsupported)
	case d.info.State != driver.StateOnline:
		return "", fmt.Errorf("fakeapi: %s: %w", d.info.Name, driver.ErrOffline)
	case d.companion == "":
		return driver.CompanionOn, nil
	}
	return d.companion, nil
}

// InstallCompanion installs the fake companion app, taking a few seconds
// as a real install with the phone's prompt does.
func (a *API) InstallCompanion(ctx context.Context, deviceID string) error {
	if _, err := a.CompanionState(ctx, deviceID); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(float64(4*time.Second) / a.opt.Speed)):
	}
	a.mu.Lock()
	d := a.device(deviceID)
	if d != nil && d.installErr != nil {
		err := d.installErr
		a.mu.Unlock()
		return err
	}
	if d != nil {
		// Its service is off again after the check: Pinky turns it on only
		// while an agent works.
		d.companion = driver.CompanionReady
		a.installs = append(a.installs, deviceID)
	}
	a.mu.Unlock()
	a.emit(gateway.Event{Kind: gateway.EventDevices, DeviceID: deviceID})
	return nil
}

// Installs lists the phones InstallCompanion installed the app on.
func (a *API) Installs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.installs...)
}

// --- background displays ---

// SetBackground sets whether agents work on a phone's background display.
func (a *API) SetBackground(deviceID string, on bool) error {
	a.mu.Lock()
	d := a.device(deviceID)
	if d == nil {
		a.mu.Unlock()
		return fmt.Errorf("fakeapi: no device %q: %w", deviceID, driver.ErrNotFound)
	}
	if !a.features(d).Background {
		a.mu.Unlock()
		return fmt.Errorf("background displays: %w", driver.ErrUnsupported)
	}
	d.background = on
	a.mu.Unlock()
	a.emit(gateway.Event{Kind: gateway.EventDevices, DeviceID: deviceID})
	return nil
}

// Background reports whether agents work on a phone's background display.
func (a *API) Background(deviceID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	d := a.device(deviceID)
	return d != nil && d.background
}

// --- connections ---

// Connections describes how an iPhone can be connected: CoreDevice works
// (iOS 27), the WebDriverAgent helper is not installed.
func (a *API) Connections(_ context.Context, deviceID string) (driver.Connections, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	d := a.device(deviceID)
	if d == nil {
		return driver.Connections{}, fmt.Errorf("fakeapi: no device %q: %w", deviceID, driver.ErrNotFound)
	}
	if d.info.Platform != driver.IOS {
		return driver.Connections{}, fmt.Errorf("connections: %w", driver.ErrUnsupported)
	}
	out := driver.Connections{Chosen: d.way, Ways: []driver.Connection{
		{ID: driver.ConnectionAuto, Available: true},
		{ID: "coredevice", Available: true},
		{ID: "wda", Reason: "not_installed", Detail: "The WebDriverAgent helper is not installed on this iPhone."},
	}}
	if d.info.State == driver.StateOnline {
		out.Current = d.info.Backend
	}
	return out, nil
}

// SetConnection chooses how an iPhone is connected. The helper is not
// installed, so choosing it leaves the phone needing setup.
func (a *API) SetConnection(deviceID, way string) error {
	a.mu.Lock()
	d := a.device(deviceID)
	if d == nil {
		a.mu.Unlock()
		return fmt.Errorf("fakeapi: no device %q: %w", deviceID, driver.ErrNotFound)
	}
	if d.info.Platform != driver.IOS {
		a.mu.Unlock()
		return fmt.Errorf("connections: %w", driver.ErrUnsupported)
	}
	d.way = way
	if way == "wda" {
		d.info.State, d.info.Detail = driver.StateNeedsSetup, "The WebDriverAgent helper is not installed on this iPhone. Install it once from Xcode, or choose another connection in Settings > Phones."
	} else {
		d.info.State, d.info.Detail, d.info.Backend = driver.StateOnline, "", "coredevice"
	}
	a.mu.Unlock()
	a.emit(gateway.Event{Kind: gateway.EventDevices, DeviceID: deviceID})
	return nil
}

// AgentStream is the agent's display while a task runs on a phone that
// works in the background: a test source of its own, so it looks unlike
// the phone's screen.
func (a *API) AgentStream(ctx context.Context, deviceID string, opt driver.StreamOptions) (driver.Stream, error) {
	a.mu.Lock()
	d := a.device(deviceID)
	if d == nil || !d.background || d.task == nil || isFinished(d.task.view.State) {
		a.mu.Unlock()
		return nil, fmt.Errorf("background displays: %w", driver.ErrNotFound)
	}
	w, h, codec := d.width, d.height, d.codec
	a.mu.Unlock()
	if opt.MaxSize > 0 && max(w, h) > opt.MaxSize {
		s := float64(opt.MaxSize) / float64(max(w, h))
		w, h = int(float64(w)*s)&^1, int(float64(h)*s)&^1
	}
	return startStream(ctx, a.opt.FFmpeg, codec, "smptebars", w, h, 30)
}

// --- recordings, retention and export ---

// SetRecording turns recording of tasks on or off; tasks that finish
// while it is off have no recording.
func (a *API) SetRecording(on bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.noRecording = !on
}

// SetRetention keeps finished tasks for days days, deleting older ones
// now; 0 keeps everything.
func (a *API) SetRetention(days int) {
	a.mu.Lock()
	a.keepDays = days
	changed := a.pruneLocked()
	a.mu.Unlock()
	if changed {
		a.emit(gateway.Event{Kind: gateway.EventTasks})
	}
}

func (a *API) pruneLocked() bool {
	if a.keepDays <= 0 {
		return false
	}
	cut := a.opt.Now().AddDate(0, 0, -a.keepDays)
	kept := a.history[:0]
	for _, t := range a.history {
		if t.Ended.IsZero() || t.Ended.After(cut) {
			kept = append(kept, t)
		}
	}
	changed := len(kept) != len(a.history)
	a.history = kept
	return changed
}

// Retention reports what SetRetention set, and Recording what SetRecording
// did, for tests.
func (a *API) Retention() (days int, recording bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.keepDays, !a.noRecording
}

// Recording returns a finished task's recording: its length, with a mark
// per step. The seeded boarding pass task has none, as when recording
// paused for a blocked app.
func (a *API) Recording(ctx context.Context, taskID string) (gateway.RecordingView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.history {
		if t.ID != taskID {
			continue
		}
		if a.unrecorded[t.ID] || t.ID == "h-gate" {
			break
		}
		length := t.Ended.Sub(t.Started)
		if length < 5*time.Second {
			length = 5 * time.Second
		}
		rec := gateway.RecordingView{TaskID: t.ID, Start: t.Started, Length: length}
		for i, s := range t.Steps {
			at := s.At.Sub(t.Started)
			if s.At.IsZero() || at < 0 {
				at = length * time.Duration(i) / time.Duration(max(1, len(t.Steps)))
			}
			text, _, _ := strings.Cut(s.Text, "\n")
			rec.Marks = append(rec.Marks, gateway.RecordingMark{At: min(at, length), Step: s.Index, Text: text})
		}
		return rec, nil
	}
	return gateway.RecordingView{}, fmt.Errorf("recordings: %w", driver.ErrNotFound)
}

// RecordingOpens lists where OpenRecording was asked to start, for tests.
func (a *API) RecordingOpens() []time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]time.Duration(nil), a.recOpens...)
}

// OpenRecording plays a fake recording from from: a drawn picture of the
// step at each moment, at the pace it was recorded, with PTS counted from
// the recording's start, as recording.Open sends them.
func (a *API) OpenRecording(ctx context.Context, rec gateway.RecordingView, from time.Duration) (driver.Stream, error) {
	a.mu.Lock()
	a.recOpens = append(a.recOpens, from)
	a.mu.Unlock()
	from = max(0, min(from, rec.Length))
	const w, h = 540, 1200
	ctx, cancel := context.WithCancel(ctx)
	start := time.Now()
	inner, err := video.FrameStream(ctx, w, h, 15, func() image.Image {
		return drawRecording(rec, from+time.Since(start), w, h)
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%v: %w", err, driver.ErrUnsupported)
	}
	s := &recStream{inner: inner, cancel: cancel, packets: make(chan driver.Packet, 64)}
	go s.forward(from, rec.Length-from)
	return s, nil
}

// recStream ends a frame stream at the recording's end and counts its PTS
// from the recording's start.
type recStream struct {
	inner   *video.TestStream
	cancel  context.CancelFunc
	packets chan driver.Packet
	mu      sync.Mutex
	err     error
}

func (s *recStream) forward(from, left time.Duration) {
	defer close(s.packets)
	end := time.After(left)
	for {
		select {
		case p, ok := <-s.inner.Packets():
			if !ok {
				s.mu.Lock()
				s.err = s.inner.Err()
				s.mu.Unlock()
				return
			}
			p.PTS += from
			s.packets <- p
		case <-end:
			s.mu.Lock()
			s.err = io.EOF
			s.mu.Unlock()
			s.cancel()
			s.inner.Close()
			return
		}
	}
}

func (s *recStream) Codec() driver.Codec           { return s.inner.Codec() }
func (s *recStream) Size() (int, int)              { return s.inner.Size() }
func (s *recStream) Packets() <-chan driver.Packet { return s.packets }
func (s *recStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
func (s *recStream) Close() error {
	s.cancel()
	return s.inner.Close()
}

// drawRecording draws the moment at of a fake recording: a color per
// step, the step's number in large digits, the time, and a progress bar.
func drawRecording(rec gateway.RecordingView, at time.Duration, w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	idx := 0
	for i, m := range rec.Marks {
		if m.At <= at {
			idx = i
		}
	}
	colors := []color.RGBA{{38, 70, 140, 255}, {32, 112, 96, 255}, {130, 60, 120, 255}, {150, 92, 30, 255}, {60, 60, 72, 255}, {44, 104, 150, 255}}
	bg := colors[idx%len(colors)]
	fill(img, image.Rect(0, 0, w, h), bg)
	fill(img, image.Rect(0, 0, w, 56), color.RGBA{bg.R / 2, bg.G / 2, bg.B / 2, 255}) // a status bar
	white := color.RGBA{255, 255, 255, 255}
	step := fmt.Sprint(idx + 1)
	if len(rec.Marks) == 0 {
		step = "0"
	}
	drawDigits(img, step, w/2, h/2-140, 220, white)
	secs := int(at / time.Second)
	drawDigits(img, fmt.Sprintf("%d:%02d", secs/60, secs%60), w/2, h/2+160, 90, white)
	frac := 0.0
	if rec.Length > 0 {
		frac = float64(at) / float64(rec.Length)
	}
	fill(img, image.Rect(40, h-90, w-40, h-78), color.RGBA{255, 255, 255, 70})
	fill(img, image.Rect(40, h-90, 40+int(float64(w-80)*min(frac, 1)), h-78), white)
	return img
}

func fill(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(img.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
		}
	}
}

// drawDigits draws digits and colons as seven-segment figures, centered
// at cx, h pixels high from top.
func drawDigits(img *image.RGBA, s string, cx, top, h int, c color.RGBA) {
	segs := map[rune]string{'0': "abcdef", '1': "bc", '2': "abged", '3': "abgcd", '4': "fgbc", '5': "afgcd",
		'6': "afgedc", '7': "abc", '8': "abcdefg", '9': "abcdfg"}
	dw, t := h/2, max(h/10, 2)
	gap := dw / 3
	total := 0
	for _, r := range s {
		if r == ':' {
			total += t + gap
		} else {
			total += dw + gap
		}
	}
	x := cx - (total-gap)/2
	half := h / 2
	for _, r := range s {
		if r == ':' {
			fill(img, image.Rect(x, top+h/3-t/2, x+t, top+h/3+t/2), c)
			fill(img, image.Rect(x, top+2*h/3-t/2, x+t, top+2*h/3+t/2), c)
			x += t + gap
			continue
		}
		for _, sg := range segs[r] {
			var rect image.Rectangle
			switch sg {
			case 'a':
				rect = image.Rect(x, top, x+dw, top+t)
			case 'b':
				rect = image.Rect(x+dw-t, top, x+dw, top+half)
			case 'c':
				rect = image.Rect(x+dw-t, top+half, x+dw, top+h)
			case 'd':
				rect = image.Rect(x, top+h-t, x+dw, top+h)
			case 'e':
				rect = image.Rect(x, top+half, x+t, top+h)
			case 'f':
				rect = image.Rect(x, top, x+t, top+half)
			case 'g':
				rect = image.Rect(x, top+half-t/2, x+dw, top+half+t/2)
			}
			fill(img, rect, c)
		}
		x += dw + gap
	}
}

// ExportHistory writes an HTML report and a JSON file of the tasks (every
// finished one when taskIDs is empty) into dir.
func (a *API) ExportHistory(ctx context.Context, taskIDs []string, dir string) (string, error) {
	tasks, err := a.History(ctx, gateway.HistoryQuery{})
	if err != nil {
		return "", err
	}
	if len(taskIDs) > 0 {
		want := map[string]bool{}
		for _, id := range taskIDs {
			want[id] = true
		}
		kept := tasks[:0]
		for _, t := range tasks {
			if want[t.ID] {
				kept = append(kept, t)
			}
		}
		tasks = kept
	}
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "pinky-history.json"), append(data, '\n'), 0o644); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("<!doctype html><meta charset=utf-8><title>Pinky history</title><h1>Pinky history</h1>")
	for _, t := range tasks {
		fmt.Fprintf(&b, "<h2>%s</h2><p>%s · %s</p><ol>", html.EscapeString(t.Title), html.EscapeString(t.Agent), html.EscapeString(t.DeviceName))
		for _, s := range t.Steps {
			fmt.Fprintf(&b, "<li>%s</li>", html.EscapeString(s.Text))
		}
		b.WriteString("</ol>")
	}
	path := filepath.Join(dir, "pinky-history.html")
	a.mu.Lock()
	a.exports = append(a.exports, Export{IDs: append([]string(nil), taskIDs...), Dir: dir})
	a.mu.Unlock()
	return path, os.WriteFile(path, []byte(b.String()), 0o644)
}

// Export is one ExportHistory call.
type Export struct {
	IDs []string
	Dir string
}

// Exports lists the ExportHistory calls, for tests.
func (a *API) Exports() []Export {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Export(nil), a.exports...)
}
