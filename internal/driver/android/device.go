package android

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/driver/android/adb"
	"github.com/DIYgod/pinky/internal/driver/android/companion"
	"github.com/DIYgod/pinky/internal/driver/android/scrcpy"
	"github.com/DIYgod/pinky/internal/screen"
)

// Device is an open Android phone. It keeps one scrcpy session for the
// main display, started on first use and restarted if it dies, which
// carries input, the video stream and settle detection.
type Device struct {
	// phoneID is the id the provider lists the phone by, when it differs
	// from the adb serial in info.ID (a phone on Wi-Fi).
	phoneID string
	cfg     Config
	client  *adb.Client
	info    driver.Info
	props   deviceProps

	mu     sync.Mutex
	main   *live
	closed bool
	// noVideo says why the phone's video encoder failed, if it did; the
	// main session then runs without video (input, typing and the
	// clipboard still work) and Stream fails until videoRetry has passed.
	noVideo   string
	noVideoAt time.Time
	videoSeen bool // a session on this device delivered a frame
	hevcSeen  bool // an H.265 session delivered a frame
	noHEVC    bool // the phone has no working H.265 encoder: use H.264

	dumpMu   sync.Mutex // uiautomator allows one dump at a time
	dumpFile bool       // this phone cannot dump to /dev/tty

	stateMu   sync.Mutex
	natW      int // display size in the natural orientation, from `wm size`
	natH      int
	sizeAt    time.Time
	curW      int // current display size, for sessions without video
	curH      int
	curAt     time.Time
	syncV2    bool // the phone has the v2 sync requests (stat_v2, ls_v2)
	syncKnown bool
	awakeAt   time.Time // when the screen was last seen on
	longPress time.Duration
	imeKeys   bool      // ASCII may be typed as key events
	imeAt     time.Time // when imeKeys was decided

	tapPoint screen.Point // the last tap, for typing into the field under it
	tapAt    time.Time

	appsMu      sync.Mutex
	apps        []driver.App
	appsAt      time.Time
	appsLoading atomic.Bool

	comp   companionLink
	person personWatch
	// compRun, set by tests, stands in for adb in the companion's clients.
	compRun companion.Runner

	bgMu sync.Mutex
	bgs  map[*Background]struct{} // open background displays
	dk   *dark                    // keeps the phone dark for them (dark.go)
	// sharedSeen: a display of this phone shared the main display's power
	// before Android 17.
	sharedSeen atomic.Bool
}

func newDevice(cfg Config, info driver.Info, props deviceProps) *Device {
	return &Device{cfg: cfg, client: cfg.Client, info: info, props: props}
}

// Info implements driver.Device, with the id the provider lists the phone
// by (see phoneID); the device itself talks to adb by Serial.
func (d *Device) Info() driver.Info {
	info := d.info
	if d.phoneID != "" {
		info.ID = d.phoneID
	}
	return info
}

// Serial returns the adb serial.
func (d *Device) Serial() string { return d.info.ID }

// Close implements driver.Device. It closes the background displays still
// open, which may wait for the phone to time out (see Background.Close),
// and ends the scrcpy session; the server exits and removes itself from
// the phone.
func (d *Device) Close() error {
	var wg sync.WaitGroup
	for _, b := range d.backgrounds() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Close()
		}()
	}
	wg.Wait()
	if dk := d.darkIfAny(); dk != nil {
		dk.close()
	}
	d.mu.Lock()
	d.closed = true
	l := d.main
	d.main = nil
	d.mu.Unlock()
	d.closeCompanion()
	d.person.close()
	if l != nil {
		l.close(errors.New("android: device closed"))
	}
	return nil
}

func (d *Device) addBackground(b *Background) {
	d.bgMu.Lock()
	defer d.bgMu.Unlock()
	if d.bgs == nil {
		d.bgs = map[*Background]struct{}{}
	}
	d.bgs[b] = struct{}{}
}

func (d *Device) removeBackground(b *Background) {
	d.bgMu.Lock()
	defer d.bgMu.Unlock()
	delete(d.bgs, b)
}

func (d *Device) backgrounds() []*Background {
	d.bgMu.Lock()
	defer d.bgMu.Unlock()
	var out []*Background
	for b := range d.bgs {
		out = append(out, b)
	}
	return out
}

// darkPanel reports whether Pinky keeps the phone awake with its panel off
// for a background display (dark.go): the display power controller then
// reports the screen on.
func (d *Device) darkPanel() bool {
	dk := d.darkIfAny()
	return dk != nil && dk.on.Load()
}

// undark turns the panel back on when Pinky keeps the phone dark, for work
// on the main screen, and reports whether it did.
func (d *Device) undark() bool {
	dk := d.darkIfAny()
	return dk != nil && dk.undark()
}

// baseOptions are the session options when no stream asked for others.
func (d *Device) baseOptions() scrcpy.Options {
	o := scrcpy.Options{
		MaxSize:      d.cfg.MaxSize,
		MaxFPS:       float64(d.cfg.MaxFPS),
		VideoBitRate: d.cfg.VideoBitRate,
		VideoCodec:   d.cfg.VideoCodec,
	}
	if d.info.Connection == "wifi" {
		// Wireless debugging carries the stream over TLS, and a phone's
		// Wi-Fi gives it far less than its link speed: 4 to 14 Mbit/s on
		// the Mi 10 Pro (1200 Mbit/s link), less while it dozes. scrcpy's
		// 8 Mbit/s at 60 frames a second queued up for seconds there, so
		// the mirror lagged behind the phone. Ask for 2.5 Mbit/s at 30
		// frames a second, in H.265, which keeps more picture in it. The
		// resolution stays: the mirror and the agent's coordinates are in
		// the phone's pixels. Measurements: docs/notes/integration.md.
		if o.VideoBitRate == 0 {
			o.VideoBitRate = wifiBitRate
		}
		if o.MaxFPS == 0 {
			o.MaxFPS = wifiMaxFPS
		}
		if o.VideoCodec == "" {
			o.VideoCodec = "h265"
		}
	}
	if o.VideoCodec == "h265" && d.noHEVC {
		o.VideoCodec = ""
	}
	return o
}

// The stream over wireless debugging (see baseOptions).
const (
	wifiBitRate = 2_500_000
	wifiMaxFPS  = 30
)

// session returns the running main-display session, starting one if
// needed.
func (d *Device) session(ctx context.Context) (*live, error) {
	return d.sessionWith(ctx, nil)
}

// videoRetry is how long Stream refuses after the encoder failed, before
// trying video again.
const videoRetry = time.Minute

// sessionWith returns a running session; with want set, one started with
// exactly those options, restarting the current one if it differs.
//
// The first video session of a device waits up to a second for its first
// frame. If the server's encoder fails instead (the Android 10 emulator's
// does, and so does its own screenrecord), the device falls back to a
// session without video, so input still works, and logs why once.
func (d *Device) sessionWith(ctx context.Context, want *scrcpy.Options) (*live, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, errors.New("android: device closed")
	}
	if l := d.main; l != nil && l.ended() {
		d.noteVideoFailureLocked(l)
	}
	noVideo := d.noVideo != ""
	if want != nil && noVideo {
		// Refuse before touching the running session, which input uses.
		if time.Since(d.noVideoAt) < videoRetry {
			return nil, fmt.Errorf("android: the phone's video encoder fails (%s): %w", d.noVideo, driver.ErrUnsupported)
		}
		noVideo = false // try again
	}
	if l := d.main; l != nil && !l.ended() {
		if want == nil || l.video && sameVideo(l.opt, *want) {
			return l, nil
		}
		d.cfg.logf("android: %s: restarting the session for new stream options", d.info.ID)
		// After the gesture in progress, so that no tap is cut off.
		l.replace(errors.New("android: session restarted with new options"))
	}
	opt := d.baseOptions()
	if want != nil {
		opt = *want
	}
	if opt.VideoCodec == "h265" && d.noHEVC {
		opt.VideoCodec = "" // H.265 failed since the options were made
	}
	if noVideo {
		opt = scrcpy.Options{NoVideo: true}
	}
	l, err := d.startMain(ctx, opt)
	if err != nil {
		return nil, err
	}
	if l.video && opt.VideoCodec == "h265" && !d.hevcSeen {
		// Some phones have no H.265 encoder, or one that fails: use H.264.
		select {
		case <-l.firstFrame:
			d.videoSeen, d.hevcSeen = true, true
		case <-l.done:
			if why := l.videoFailure(); why != "" {
				d.cfg.logf("android: %s: the H.265 encoder failed (%s); using H.264", d.info.ID, why)
				d.noHEVC = true
				opt.VideoCodec = ""
				if l, err = d.startMain(ctx, opt); err != nil {
					return nil, err
				}
			}
		case <-time.After(time.Second):
		case <-ctx.Done():
			l.close(ctx.Err())
			return nil, ctx.Err()
		}
	}
	if l.video && !d.videoSeen {
		select {
		case <-l.firstFrame:
			d.videoSeen = true
			d.hevcSeen = d.hevcSeen || opt.VideoCodec == "h265"
		case <-l.done:
			if !d.noteVideoFailureLocked(l) {
				return nil, l.failure()
			}
			if want != nil {
				return nil, fmt.Errorf("android: the phone's video encoder fails (%s): %w", d.noVideo, driver.ErrUnsupported)
			}
			if l, err = d.startMain(ctx, scrcpy.Options{NoVideo: true}); err != nil {
				return nil, err
			}
		case <-time.After(time.Second):
			// No frame yet: the screen may be off. Keep the session; a
			// failure later is noted when it ends.
		case <-ctx.Done():
			l.close(ctx.Err())
			return nil, ctx.Err()
		}
	}
	if l.video {
		d.noVideo = ""
	}
	d.main = l
	return l, nil
}

// startMain starts a session on the main display.
func (d *Device) startMain(ctx context.Context, opt scrcpy.Options) (*live, error) {
	natW, natH, err := d.naturalSize(ctx)
	if err != nil {
		return nil, err
	}
	l, err := startLive(ctx, &d.cfg, d.client, d.info.ID, opt, func(vw, vh int) (int, int) {
		return orient(natW, natH, vw, vh)
	}, d.displaySize)
	if err != nil {
		return nil, wrapErr(err)
	}
	return l, nil
}

// noteVideoFailureLocked records an ended session's encoder failure, and
// reports whether the phone's video encoder fails; an H.265 session that
// failed only makes later sessions use H.264. d.mu must be held.
func (d *Device) noteVideoFailureLocked(l *live) bool {
	reason := l.videoFailure()
	if reason == "" {
		return false
	}
	if l.opt.VideoCodec == "h265" {
		if !d.noHEVC {
			d.cfg.logf("android: %s: the H.265 encoder failed (%s); using H.264", d.info.ID, reason)
		}
		d.noHEVC = true
		return false
	}
	if d.noVideo == "" {
		d.cfg.logf("android: %s: the video encoder failed (%s); using a session without video: input works, the stream does not", d.info.ID, reason)
	}
	d.noVideo, d.noVideoAt = reason, time.Now()
	return true
}

// displaySize returns the main display's current size, rotation included,
// for sessions without video, whose touches are in display pixels.
func (d *Device) displaySize(ctx context.Context) (int, int, error) {
	d.stateMu.Lock()
	if d.curW > 0 && time.Since(d.curAt) < 2*time.Second {
		w, h := d.curW, d.curH
		d.stateMu.Unlock()
		return w, h, nil
	}
	d.stateMu.Unlock()
	out, err := d.client.Shell(ctx, d.info.ID, "dumpsys window displays | grep -E ' cur=[0-9]+x[0-9]+'")
	if err != nil {
		return 0, 0, wrapErr(err)
	}
	w, h, ok := parseCurSize(string(out))
	if !ok {
		// Fall back to the natural size: portrait phones, mostly.
		if w, h, err = d.naturalSize(ctx); err != nil {
			return 0, 0, err
		}
	}
	d.stateMu.Lock()
	d.curW, d.curH, d.curAt = w, h, time.Now()
	d.stateMu.Unlock()
	return w, h, nil
}

func sameVideo(a, b scrcpy.Options) bool {
	return a.MaxSize == b.MaxSize && a.MaxFPS == b.MaxFPS && a.VideoBitRate == b.VideoBitRate &&
		a.VideoCodec == b.VideoCodec
}

// naturalSize returns the display size in the natural orientation,
// cached for a minute.
func (d *Device) naturalSize(ctx context.Context) (int, int, error) {
	d.stateMu.Lock()
	if d.natW > 0 && time.Since(d.sizeAt) < time.Minute {
		w, h := d.natW, d.natH
		d.stateMu.Unlock()
		return w, h, nil
	}
	d.stateMu.Unlock()
	out, err := d.client.Shell(ctx, d.info.ID, "wm size")
	if err != nil {
		return 0, 0, wrapErr(err)
	}
	w, h, ok := parseWMSize(string(out))
	if !ok {
		return 0, 0, fmt.Errorf("android: cannot read the display size from %q", out)
	}
	d.stateMu.Lock()
	d.natW, d.natH, d.sizeAt = w, h, time.Now()
	d.stateMu.Unlock()
	return w, h, nil
}

// live is one running scrcpy session and the goroutine that reads its
// video.
type live struct {
	cfg    *Config
	s      *scrcpy.Session
	opt    scrcpy.Options
	codec  driver.Codec
	serial string
	video  bool // the session carries video; without it, settle has no frames
	// screenSize maps the video size to the size of the screen that
	// agents' coordinates use.
	screenSize func(vw, vh int) (int, int)
	// displaySize returns the display size for sessions without video.
	displaySize func(ctx context.Context) (int, int, error)

	mu         sync.Mutex
	vw, vh     int
	sized      chan struct{} // closed at the first session header
	sizedOnce  sync.Once
	firstFrame chan struct{} // closed at the first frame packet
	frameOnce  sync.Once
	frames     int
	encoderErr string // the server's encoder failed before any frame
	sub        *stream

	settle settleTracker
	stats  streamStats

	// Input runs one gesture at a time, and a restart waits for the one in
	// progress: closing the sockets drops control messages the server has
	// not read yet, which loses taps.
	inputMu   sync.Mutex
	lastInput atomic.Int64 // Unix nanoseconds when the last gesture ended
	replaced  atomic.Bool  // the device closed it for a new session

	done chan struct{}
	once sync.Once
	err  error
}

// errReplaced means the device replaced the session before a gesture
// began: nothing was sent, so the gesture can go to the new session.
var errReplaced = errors.New("android: session replaced")

// inputGrace is how long a session stays open after its last gesture
// before the device replaces it, so the server processes that gesture.
const inputGrace = 150 * time.Millisecond

// input runs one gesture (a tap, a swipe, a key, typing) on the session.
func (l *live) input(f func() error) error {
	l.inputMu.Lock()
	defer l.inputMu.Unlock()
	if l.replaced.Load() {
		return errReplaced
	}
	err := f()
	l.lastInput.Store(time.Now().UnixNano())
	return err
}

// replace closes the session for a new one, once the gesture in progress
// has ended and the server has had time to process it.
func (l *live) replace(why error) {
	l.inputMu.Lock()
	defer l.inputMu.Unlock()
	if last := l.lastInput.Load(); last != 0 {
		if wait := inputGrace - time.Since(time.Unix(0, last)); wait > 0 {
			time.Sleep(wait)
		}
	}
	l.replaced.Store(true)
	l.close(why)
}

func startLive(ctx context.Context, cfg *Config, c *adb.Client, serial string, opt scrcpy.Options,
	screenSize func(vw, vh int) (int, int), displaySize func(ctx context.Context) (int, int, error)) (*live, error) {
	start := time.Now()
	s, err := scrcpy.Start(ctx, c, serial, opt)
	if err != nil {
		return nil, err
	}
	l := &live{
		cfg: cfg, s: s, opt: opt, serial: serial, screenSize: screenSize, displaySize: displaySize,
		video:      !opt.NoVideo,
		codec:      driver.H264,
		sized:      make(chan struct{}),
		firstFrame: make(chan struct{}),
		done:       make(chan struct{}),
	}
	if s.Codec == scrcpy.CodecH265 {
		l.codec = driver.HEVC
	}
	l.settle.fixed = cfg.SettleBytes
	what := "scrcpy session"
	if !l.video {
		what = "scrcpy session without video"
		l.sizedOnce.Do(func() { close(l.sized) })
	}
	cfg.logf("android: %s: %s started in %v", serial, what, time.Since(start).Round(time.Millisecond))
	go l.run()
	return l, nil
}

func (l *live) run() {
	if !l.video {
		<-l.s.Done()
		l.close(l.s.Err())
		return
	}
	for {
		p, err := l.s.ReadPacket()
		if err != nil {
			l.mu.Lock()
			if l.frames == 0 {
				l.encoderErr = l.s.VideoFailure()
			}
			l.mu.Unlock()
			l.close(err)
			return
		}
		now := time.Now()
		if p.Session {
			l.mu.Lock()
			l.vw, l.vh = p.Width, p.Height
			l.mu.Unlock()
			l.sizedOnce.Do(func() { close(l.sized) })
			l.settle.resize(now, p.Width, p.Height)
			continue
		}
		l.mu.Lock()
		l.frames++
		l.mu.Unlock()
		l.frameOnce.Do(func() { close(l.firstFrame) })
		change := l.settle.packet(now, p.PTS, len(p.Data), p.Config, p.KeyFrame)
		l.stats.add(now, p.PTS, len(p.Data), p.Config, p.KeyFrame, change)
		l.mu.Lock()
		sub := l.sub
		l.mu.Unlock()
		if sub != nil {
			sub.push(p)
		}
	}
}

// videoFailure returns why the server's encoder failed, if the session
// ended that way before delivering a frame.
func (l *live) videoFailure() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.encoderErr
}

// close ends the session and any stream on it.
func (l *live) close(err error) {
	l.once.Do(func() {
		l.mu.Lock()
		l.err = err
		sub := l.sub
		l.sub = nil
		l.mu.Unlock()
		close(l.done)
		l.s.Close()
		if sub != nil {
			sub.end(fmt.Errorf("%w: %v", driver.ErrOffline, err))
		}
	})
}

func (l *live) ended() bool {
	select {
	case <-l.done:
		return true
	default:
		return false
	}
}

// failure is the error to return for a session that ended.
func (l *live) failure() error {
	l.mu.Lock()
	err := l.err
	l.mu.Unlock()
	if errors.Is(err, scrcpy.ErrClosed) {
		return err
	}
	return fmt.Errorf("%w: scrcpy session ended: %v", driver.ErrOffline, err)
}

// videoSize waits for the first session header and returns the current
// video size; without video, the display's size.
func (l *live) videoSize(ctx context.Context) (int, int, error) {
	if !l.video {
		return l.displaySize(ctx)
	}
	select {
	case <-l.sized:
	case <-l.done:
		return 0, 0, l.failure()
	case <-ctx.Done():
		return 0, 0, ctx.Err()
	case <-time.After(5 * time.Second):
		return 0, 0, errors.New("android: no video from the scrcpy session")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.vw, l.vh, nil
}

// position maps a screen point to the video frame the server expects.
func (l *live) position(ctx context.Context, p screen.Point) (scrcpy.Position, error) {
	vw, vh, err := l.videoSize(ctx)
	if err != nil {
		return scrcpy.Position{}, err
	}
	sw, sh := vw, vh
	if l.video {
		sw, sh = l.screenSize(vw, vh)
	}
	x, y := p.X, p.Y
	if sw > 0 && sh > 0 && (sw != vw || sh != vh) {
		x = (x*vw + sw/2) / sw
		y = (y*vh + sh/2) / sh
	}
	x = min(max(x, 0), vw-1)
	y = min(max(y, 0), vh-1)
	return scrcpy.Position{X: int32(x), Y: int32(y), Width: uint16(vw), Height: uint16(vh)}, nil
}

// send writes a control message, mapping failures to driver errors.
func (l *live) send(msg []byte) error {
	if err := l.s.Send(msg); err != nil {
		return l.failure()
	}
	return nil
}

var (
	_ driver.Device        = (*Device)(nil)
	_ driver.Toucher       = (*Device)(nil)
	_ driver.LockReporter  = (*Device)(nil)
	_ driver.Clipboard     = (*Device)(nil)
	_ driver.Settler       = (*Device)(nil)
	_ driver.PersonWatcher = (*Device)(nil)
)
