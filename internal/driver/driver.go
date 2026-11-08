// Package driver defines what a platform driver provides: device discovery,
// observation, input and a video stream. The gateway never branches on the
// platform; it only talks to these interfaces.
package driver

import (
	"context"
	"errors"
	"image"
	"io"
	"time"

	"github.com/DIYgod/pinky/internal/screen"
)

// Platform is a phone platform.
type Platform string

// Platforms.
const (
	Android Platform = "android"
	IOS     Platform = "ios"
)

// State is a device's connection state.
type State string

// Device states.
const (
	StateOnline       State = "online"
	StateOffline      State = "offline"
	StateUnauthorized State = "unauthorized" // the phone has not trusted this computer yet
	StateNeedsSetup   State = "needs_setup"  // e.g. Developer Mode off; Detail says what to do
)

// Info describes a device.
type Info struct {
	ID         string // adb serial or iOS UDID; stable across reconnects
	Platform   Platform
	Name       string // "Pixel 9", or the name the owner gave the iPhone
	Model      string
	OSVersion  string
	Connection string // "usb" or "wifi"
	Backend    string // "scrcpy", "coredevice", "wda", "mirroring", "fake"
	State      State
	Detail     string // a plain sentence when State is not online
	// Setup names the step a phone that is not online waits for, one of
	// the Setup constants, so a client can walk the person through it in
	// its own words; "" when the driver does not say. Detail says the same
	// in a sentence.
	Setup string
}

// Setup steps, in Info.Setup.
const (
	SetupTrust         = "trust"          // unlock the phone, tap Trust and enter its passcode
	SetupDeveloperMode = "developer_mode" // turn on Developer Mode, which restarts the phone
	SetupUnlock        = "unlock"         // unlock the phone so the computer can finish preparing it
	SetupPreparing     = "preparing"      // the computer is preparing the phone; nothing to do but wait
	SetupNoDiskImage   = "no_disk_image"  // the computer has no developer disk image; Detail says where to get one
	SetupDiskImage     = "disk_image"     // the developer disk image did not mount; Detail says why
	SetupUpdate        = "update"         // the phone's OS is too old for Pinky
)

// Provider discovers and opens the devices of one platform.
type Provider interface {
	Platform() Platform
	List(ctx context.Context) ([]Info, error)
	Open(ctx context.Context, id string) (Device, error)
}

// HostChecker is a Provider that needs software on the computer (usbmuxd
// for iPhones) and says when it is missing, since a provider without it
// lists no phones at all.
type HostChecker interface {
	// CheckHost reports what the computer lacks; the zero HostCheck means
	// nothing.
	CheckHost() HostCheck
}

// HostCheck is what HostChecker found missing.
type HostCheck struct {
	// Missing is a code clients put into words: "apple_devices" (Windows:
	// Apple's iPhone driver, from the Apple Devices app or iTunes),
	// "usbmuxd" (Linux: the usbmuxd package), "usbmuxd_stopped" (Linux:
	// installed, but not running; it starts when an iPhone is plugged in).
	Missing string
	Detail  string // the same in a sentence
}

// Device is an open, long-lived session with one phone. Methods must be safe
// to call from several goroutines; implementations serialize as needed.
type Device interface {
	Info() Info
	// Observe reads the current screen. Refs are assigned by the gateway.
	Observe(ctx context.Context, opt ObserveOptions) (*screen.Screen, error)
	// Screenshot returns the screen in the same pixel space as Observe.
	Screenshot(ctx context.Context) (image.Image, error)
	Tap(ctx context.Context, p screen.Point, opt TapOptions) error
	Swipe(ctx context.Context, from, to screen.Point, dur time.Duration) error
	// Type enters text into the focused field.
	Type(ctx context.Context, text string, opt TypeOptions) error
	Key(ctx context.Context, k Key) error
	// Launch starts an app by package name, bundle id or display name.
	Launch(ctx context.Context, app string) error
	Apps(ctx context.Context) ([]App, error)
	// Stream starts the live video stream. One stream per device at a time.
	Stream(ctx context.Context, opt StreamOptions) (Stream, error)
	Close() error
}

// ObserveOptions tune Observe.
type ObserveOptions struct {
	// OCR asks the driver to add OCR elements for areas the tree misses.
	OCR bool
}

// TapOptions tune Tap.
type TapOptions struct {
	LongPress bool
}

// TypeOptions tune Type.
type TypeOptions struct {
	Append bool // keep the field's current text
	Submit bool // press Enter afterwards
}

// App is an installed app.
type App struct {
	ID     string // package name or bundle id
	Name   string
	System bool
}

// Key is a hardware or navigation key.
type Key string

// Keys agents may press. Drivers return ErrUnsupported for keys their
// platform lacks (for example KeyRecents on iPhone).
const (
	KeyBack          Key = "back"
	KeyHome          Key = "home"
	KeyRecents       Key = "recents"
	KeyEnter         Key = "enter"
	KeyTab           Key = "tab"
	KeyDelete        Key = "delete"
	KeyForwardDelete Key = "forward_delete"
	KeyEscape        Key = "escape"
	KeyDpadUp        Key = "dpad_up"
	KeyDpadDown      Key = "dpad_down"
	KeyDpadLeft      Key = "dpad_left"
	KeyDpadRight     Key = "dpad_right"
	KeyPageUp        Key = "page_up"
	KeyPageDown      Key = "page_down"
)

// StreamOptions tune the video stream.
type StreamOptions struct {
	MaxSize int // longest side in pixels; 0 = native
	MaxFPS  int // 0 = device default
}

// Codec is a video codec.
type Codec string

// Codecs.
const (
	H264 Codec = "h264"
	HEVC Codec = "hevc"
)

// Packet is one encoded unit of the video stream.
type Packet struct {
	Data     []byte // Annex B
	PTS      time.Duration
	Config   bool // codec configuration (SPS/PPS/VPS)
	KeyFrame bool
}

// Stream is a live encoded video stream.
type Stream interface {
	Codec() Codec
	Size() (width, height int)
	Packets() <-chan Packet // closed when the stream ends; Err says why
	Err() error
	Close() error
}

// Optional capabilities. The gateway checks for them with type assertions.

// TouchAction is a raw touch phase, used for the person's input on the mirror.
type TouchAction string

// Touch actions.
const (
	TouchDown TouchAction = "down"
	TouchMove TouchAction = "move"
	TouchUp   TouchAction = "up"
)

// Toucher injects raw touches, for drags on the mirror. The point is in the
// pixels of the picture the device's Stream sends (at MaxSize 0), because
// that is all the mirror knows. Drivers whose video size differs from their
// screen size (iOS pads its video) convert to screen pixels themselves.
type Toucher interface {
	Touch(ctx context.Context, a TouchAction, p screen.Point) error
}

// PersonWatcher reports input the person makes on the phone itself, so the
// gateway can pause an agent's task when someone picks the phone up. Input
// that Pinky injects must not be reported.
type PersonWatcher interface {
	// PersonInput returns a channel that receives a value, at most a few
	// times a second, while the person touches the screen or presses a key
	// on the phone. It is closed when the device closes.
	PersonInput() <-chan struct{}
}

// LockReporter reports whether the phone is locked.
type LockReporter interface {
	Locked(ctx context.Context) (bool, error)
}

// Clipboard reads and writes the phone's clipboard.
type Clipboard interface {
	SetClipboard(ctx context.Context, text string) error
	Clipboard(ctx context.Context) (string, error)
}

// Settler waits until the screen stops changing after an action, using
// whatever signal the backend has (encoded frame sizes, accessibility
// events). It returns settled=false when max passed first.
type Settler interface {
	WaitSettled(ctx context.Context, max time.Duration) (settled bool, err error)
}

// Waker turns the screen on. Pinky never keeps a phone awake: the gateway
// wakes it before an agent's step, and the phone's own screen timeout and
// lock apply between steps and tasks. Wake must not unlock, press the
// power key (which would lock a phone whose screen is on) or change a
// setting.
type Waker interface {
	// ScreenOn reports whether the display is on.
	ScreenOn(ctx context.Context) (bool, error)
	// Wake turns the display on when it is off, and does nothing when it
	// is on.
	Wake(ctx context.Context) error
}

// Unlocker enters the phone's PIN or passcode on its lock screen. Only the
// person can trigger it, from the client after Touch ID or the system's
// equivalent; agents never reach it and never see the PIN. It wakes the
// screen, brings up the PIN pad, enters pin and confirms, then returns nil
// once the lock screen is gone, ErrLocked when it is still there after the
// PIN was entered (a wrong PIN: one attempt used), or ErrPINNotEntered when
// it stopped before entering anything (the PIN pad never appeared, the
// phone refuses attempts for now). It never retries: phones lock out or
// wipe after repeated failures.
type Unlocker interface {
	Unlock(ctx context.Context, pin string) error
}

// DiskImageDownloader is a Provider that can download the developer disk
// image a computer without Xcode lacks (iPhones), from a third-party
// mirror, when the person asks.
type DiskImageDownloader interface {
	// DiskImageMirror says where the download comes from.
	DiskImageMirror() DiskImageMirror
	// DownloadDiskImage downloads the image and checks every file;
	// progress, when not nil, gets the bytes done and the total. A file
	// that is not the one expected fails with ErrDiskImageMismatch.
	DownloadDiskImage(ctx context.Context, progress func(done, total int64)) error
}

// DiskImageMirror describes what DiskImageDownloader downloads.
type DiskImageMirror struct {
	Name  string // where it comes from, such as "github.com/doronz88/DeveloperDiskImage"
	Image string // which image, such as "Xcode 27.0 beta 4 (27A5228h)"
	Size  int64  // bytes
}

// ErrDiskImageMismatch is a downloaded developer disk image file that is
// not the one expected (its size or checksum differs); it is not used.
var ErrDiskImageMismatch = errors.New("the downloaded file is not the one expected")

// Checkup items and fixes only iPhones have, with the Check* and Fix*
// names. A failed item's fix can also be a Setup step: SetupDeveloperMode,
// SetupNoDiskImage.
const (
	CheckDeveloperMode = "developer_mode" // Developer Mode is on
	CheckDiskImage     = "disk_image"     // the developer disk image is mounted, and the computer can mount it again after a restart
	CheckHelper        = "helper"         // the WebDriverAgent helper answers, where Pinky drives the phone through it
	FixInstallHelper   = "install_helper" // install the WebDriverAgent helper on the phone
)

// FileInfo is one entry of the phone's Pinky folder.
type FileInfo struct {
	Path    string // slash-separated, relative to the Pinky folder
	Size    int64
	ModTime time.Time
	Dir     bool
}

// Files reads and writes the phone's Pinky folder (/sdcard/Pinky on
// Android), the only place on the phone agents may reach. Paths are
// relative to it; drivers refuse paths that leave it (.., absolute paths,
// symlinks out) with ErrNotFound.
type Files interface {
	// ListFiles lists the whole folder, recursively, creating it when it
	// does not exist.
	ListFiles(ctx context.Context) ([]FileInfo, error)
	Pull(ctx context.Context, path string, w io.Writer) error
	// Push writes a file, creating its folders, and sets its time.
	Push(ctx context.Context, path string, r io.Reader, size int64, mtime time.Time) error
}

// BackgroundOptions configure Backgrounder.OpenBackground.
type BackgroundOptions struct {
	// App is started on the new display once it exists (package, bundle id
	// or display name, as for Launch). Empty starts nothing.
	App string
}

// Backgrounder opens a second display on the phone for an agent to work
// on while the main screen stays free for the person, or off and locked
// (Android: a scrcpy virtual display). The Device it returns observes,
// acts and streams on that display only, and closes it on Close; the
// phone's own Device keeps working on the main screen.
type Backgrounder interface {
	OpenBackground(ctx context.Context, opt BackgroundOptions) (Device, error)
}

// BackgroundDefaulter says whether agents should work on a background
// display unless the person chooses otherwise: true where that display keeps
// working while the phone is off and locked (Android 13 and later), so the
// phone's own screen can stay off during tasks.
type BackgroundDefaulter interface {
	BackgroundByDefault() bool
}

// BackgroundSupporter says whether a driver.Backgrounder works on this
// phone at all (Android 10 cannot read other displays' windows). A
// Backgrounder that does not implement it does.
type BackgroundSupporter interface {
	BackgroundSupported() bool
}

// CompanionState is whether the phone's companion app is ready.
type CompanionState string

// Companion states.
const (
	CompanionOn  CompanionState = "on"  // installed, current and its service is running
	CompanionOff CompanionState = "off" // installed, but its accessibility service is off and Pinky could not turn it on
	// CompanionReady: installed and current; its service is off, and Pinky
	// turns it on while an agent works on the phone (WorkWatcher).
	CompanionReady    CompanionState = "ready"
	CompanionMissing  CompanionState = "missing"  // not installed
	CompanionOutdated CompanionState = "outdated" // installed, older than the one Pinky bundles or signed with another key
	CompanionNoAPK    CompanionState = "no_apk"   // Pinky has no APK to install (a build without it)
)

// CompanionInstaller installs or updates the phone's companion app from the
// APK bundled with Pinky. Installing happens only when the person asks in
// the client; the driver never installs anything by itself.
type CompanionInstaller interface {
	CompanionState(ctx context.Context) (CompanionState, error)
	// InstallCompanion installs or updates it and checks that its service
	// comes on, which it then is only while an agent works on the phone.
	// The phone may ask the person to allow the install (Google Play
	// Protect); the call waits for that, up to ctx.
	InstallCompanion(ctx context.Context) error
}

// WorkWatcher is told whether an agent works on the phone: from the start
// of a task to its end, except while the person is in control. A driver
// keeps on, only while one does, what agents need and the phone's apps can
// see: Android's Pinky Companion, whose accessibility service banking apps
// refuse to sign in next to. The gateway calls AgentWorking right after it
// opens the device and whenever the answer changes; it must return at once
// and do any work in the background.
type WorkWatcher interface {
	AgentWorking(on bool)
}

// Pairer pairs phones over the network: Android's wireless debugging, by
// QR code or by pairing code. Paired phones then show up in List with
// Connection "wifi".
type Pairer interface {
	// StartPairing begins pairing by QR code: the client shows QR() as a QR
	// code, the person scans it in Developer options › Wireless debugging ›
	// Pair device with QR code, and Wait returns the phone once it paired
	// and connected.
	StartPairing(ctx context.Context) (Pairing, error)
	// PairWithCode pairs with the address and six-digit code the phone
	// shows under Pair device with pairing code, then connects.
	PairWithCode(ctx context.Context, addr, code string) (Info, error)
}

// Pairing is a QR pairing in progress.
type Pairing interface {
	QR() string // the text the QR code encodes
	Wait(ctx context.Context) (Info, error)
	Close() error
}

// PairingReporter is a Pairing that says how it is going, so a client can
// explain one that takes long.
type PairingReporter interface {
	Progress() PairingProgress
}

// PairingProgress is how far a QR pairing got. The zero value says the
// phone has not shown up yet and nothing is known to be wrong.
type PairingProgress struct {
	// Found is set once the phone showed up on the network, which it does
	// once it scanned the code.
	Found bool
	// Paired is set once the phone took the pairing; the driver then
	// connects to it.
	Paired bool
	// NotAllowed is set while the system does not let Pinky look for
	// phones on the local network (on macOS, Pinky's switch under System
	// Settings › Privacy & Security › Local Network).
	NotAllowed bool
	// SearchErr says why Pinky cannot look for the phone on the network
	// itself, when it cannot; "" while it can.
	SearchErr string
}

// WiFiConnector connects to a phone that is paired with this computer at
// the address it shows under Wireless debugging (IP address & Port), for a
// network on which the driver cannot find it by itself. It returns the
// phone once it is online.
type WiFiConnector interface {
	ConnectWiFi(ctx context.Context, addr string) (Info, error)
}

// Forgetter is a Provider that connects to phones by itself, as Android's
// reconnects the phones paired over Wi-Fi. The gateway tells it which
// phones the person removed from Pinky, so it leaves them alone: Forget
// when the person removes one and, before the first List, for each one
// removed earlier; Remember once they add it back, by pairing it again or
// plugging it in.
type Forgetter interface {
	// Forget stops the provider from connecting to the phone by itself.
	Forget(id string)
	// Remember undoes Forget.
	Remember(id string)
	// Disconnect drops the phone's connections over the network that the
	// provider can drop, as the person removes it. A cable's connection
	// stays, and the phone with it.
	Disconnect(ctx context.Context, id string) error
}

// ErrPairedNotFound is wrapped by PairWithCode and Pairing.Wait when the
// phone paired but the driver could not find where to connect to it: its
// wireless debugging service did not show up on the network.
// WiFiConnector then connects with the address the person reads off the
// phone.
var ErrPairedNotFound = errors.New("the phone paired, but its wireless debugging address was not found on the network")

// ConnectionChooser is a Provider that can reach a phone in more than one
// way and lets the person choose, per phone (iPhone: Xcode's CoreDevice
// protocol, or the WebDriverAgent helper). The choice takes effect at the
// next Open; the gateway closes the phone's session to apply it.
type ConnectionChooser interface {
	// Connections describes the ways to connect a phone, in the order the
	// person sees them, automatic first.
	Connections(ctx context.Context, id string) Connections
	// SetConnection chooses one by its ID; ConnectionAuto goes back to the
	// driver's own rule.
	SetConnection(id, way string) error
}

// ConnectionAuto is the ID of the driver's own choice.
const ConnectionAuto = ""

// Connections is what ConnectionChooser.Connections says about a phone.
type Connections struct {
	Chosen  string       // the person's choice, ConnectionAuto when none
	Current string       // the way the phone is reached now (its Info.Backend), "" when it is not
	Ways    []Connection // automatic first
}

// Connection is one way to connect a phone.
type Connection struct {
	ID        string // ConnectionAuto, or a backend name such as "coredevice" or "wda"
	Available bool   // it can be used with this phone now
	// Reason says why it cannot, as a code clients put into words:
	// "os_version" (the phone's OS is too old or too new for it),
	// "not_installed" (it needs an app on the phone that is not there),
	// "needs_setup" (the phone needs a step first, such as Developer Mode),
	// "offline" (the phone does not answer).
	Reason string
	Detail string // the same in a sentence, for logs and clients without words for Reason
}

// Errors drivers return. Wrap them with fmt.Errorf("...: %w", err).
var (
	ErrUnsupported = errors.New("not supported by this device or backend")
	ErrOffline     = errors.New("device offline")
	ErrLocked      = errors.New("device locked")
	// ErrPINNotEntered: Unlock stopped before entering anything, so no
	// attempt was used.
	ErrPINNotEntered = errors.New("the PIN was not entered")
	ErrNotFound      = errors.New("not found")
)

// Checkups. A phone's checkup checks, item by item and for real, what
// Pinky needs on it (a key that does nothing to see that input works, a
// screenshot, the companion app), and says what to do about each item
// that fails. The client's checkup page and the last step of Add phone
// show it.

// CheckStatus is how one item of a checkup went.
type CheckStatus string

// Check statuses.
const (
	CheckPass CheckStatus = "pass" // it works
	CheckFail CheckStatus = "fail" // it does not work; Fix says what to do
	CheckWarn CheckStatus = "warn" // it works, with a limit the person may want to lift
	CheckSkip CheckStatus = "skip" // not checked: it does not apply, or an earlier item must pass first
)

// Check is one item of a phone's checkup.
type Check struct {
	// ID says what was checked: one of the Check* names below, or one a
	// driver adds (clients show items they do not know by Detail).
	ID     string
	Status CheckStatus
	// Fix names what to do about an item that failed or has a limit, a
	// code clients put into steps and buttons in their own words (the
	// Fix* names below); "" when there is nothing to do.
	Fix string
	// Detail is what the driver found, in its own words and in English:
	// an error, or a fact such as "adb server version 41".
	Detail string
}

// What a checkup checks, in Check.ID.
const (
	CheckHost         = "host"          // the computer has what the phone's platform needs (driver.HostChecker): Fix is what it lacks
	CheckADB          = "adb"           // Android: the adb server answers
	CheckConnection   = "connection"    // the phone is connected and answers
	CheckAuthorized   = "authorized"    // the phone allows this computer (USB debugging allowed, trusted)
	CheckCompanion    = "companion"     // Android: Pinky Companion is installed, current and on
	CheckInput        = "input"         // taps, swipes and keys reach the phone
	CheckScreen       = "screen"        // screenshots and the live picture
	CheckBackground   = "background"    // agents can work on a background display
	CheckWiFi         = "wifi"          // the phone can be paired and used over Wi-Fi
	CheckLocalNetwork = "local_network" // macOS: Pinky may look for phones on the local network
)

// What to do, in Check.Fix.
const (
	FixReconnect        = "reconnect"          // connect the phone again: plug it in, or turn Wi-Fi debugging back on
	FixAuthorize        = "authorize"          // allow this computer on the phone (Allow USB debugging, Trust)
	FixUSBPermission    = "usb_permission"     // Linux: the computer may not open the phone's USB device (udev rules)
	FixUSBDriver        = "usb_driver"         // Windows: the phone's USB debugging has no driver
	FixInstallCompanion = "install_companion"  // install Pinky Companion
	FixUpdateCompanion  = "update_companion"   // update it to the version Pinky carries
	FixTurnOnCompanion  = "turn_on_companion"  // turn its accessibility service on
	FixInputPermission  = "input_permission"   // the phone refuses input from the computer (Xiaomi's USB debugging (Security settings), ColorOS's permission monitoring)
	FixVideoEncoder     = "video_encoder"      // the phone's video encoder fails: the live picture is replaced by still pictures
	FixScreenshot       = "screenshot"         // the phone sends no screenshots
	FixNewerAndroid     = "newer_android"      // the item needs a newer Android version than the phone has
	FixUnlockedOnly     = "unlocked_only"      // background work only while the phone is unlocked (Android 11 and 12)
	FixWirelessDebug    = "wireless_debugging" // turn on Wireless debugging and pair the phone, to use it over Wi-Fi
	FixLocalNetwork     = "local_network"      // allow Pinky under Privacy & Security › Local Network
	FixLocked           = "locked"             // unlock the phone, then check again
	FixRestart          = "restart"            // restart the phone: a setting it has takes effect only then
)

// Setup steps of Android phones, in Info.Setup with the Setup constants:
// what the computer, not the phone, lacks. A checkup's fix for a phone
// that is not online is its Info.Setup.
const (
	SetupUSBPermission = FixUSBPermission // Linux: no udev rule for the phone, or not in the plugdev group
	SetupUSBDriver     = FixUSBDriver     // Windows: no driver for the phone's USB debugging interface
)

// Checkup is a phone's checkup, as its driver ran it.
type Checkup struct {
	// Family names the maker's system the phone runs, for steps that
	// differ between makers: "xiaomi" (HyperOS, MIUI), "oppo" (ColorOS,
	// OxygenOS, realme UI), "vivo" (OriginOS, Funtouch OS), "honor"
	// (MagicOS), "huawei" (EMUI, HarmonyOS), "samsung" (One UI), "pixel";
	// "" for stock Android and others, such as a Xiaomi phone that runs
	// LineageOS.
	Family string
	// System is that system and its version as the phone names it, for
	// people to read ("HyperOS 2.0", "LineageOS 23.0"); "" when unknown.
	System string
	Checks []Check
}

// Checker is a Device that runs its own checkup. It must not change
// anything on the phone the person would notice: no taps, no settings, no
// installs.
type Checker interface {
	Checkup(ctx context.Context) (Checkup, error)
}
