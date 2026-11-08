package companion

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Install installs or updates the companion app from an APK file, like
// `adb install -r`. Pinky may install and remove only its own apps.
//
// On phones with Google Play Protect checking adb installs, an install of
// an app Play does not know shows a "Send app for a security check?"
// dialog on the phone and waits for the person; prefer InstallIfChanged.
func (c *Client) Install(ctx context.Context, apk string) error {
	return c.run.Install(ctx, apk)
}

// InstallIfChanged installs the APK unless the phone already has exactly
// this file, compared by SHA-256. It reports whether it installed.
func (c *Client) InstallIfChanged(ctx context.Context, apk string) (bool, error) {
	local, err := fileSHA256(apk)
	if err != nil {
		return false, err
	}
	if remote, err := c.InstalledSHA256(ctx); err == nil && remote == local {
		return false, nil
	}
	if err := c.Install(ctx, apk); err != nil {
		return false, err
	}
	return true, nil
}

// Uninstall removes the client's build of the companion app, Pinky's own
// package and nothing else. Call ForceDisable first: when an uninstalled
// package's service is listed in enabled_accessibility_services, Android
// rewrites the setting, and the person's other entries with it.
func (c *Client) Uninstall(ctx context.Context) error {
	out, err := c.run.Shell(ctx, "pm uninstall "+c.app.Package)
	if err != nil {
		return err
	}
	if !strings.Contains(out, "Success") {
		return fmt.Errorf("pm uninstall %s: %s", c.app.Package, strings.TrimSpace(out))
	}
	return nil
}

var (
	versionNameRE = regexp.MustCompile(`(?m)^\s*versionName=(\S+)`)
	versionCodeRE = regexp.MustCompile(`(?m)\bversionCode=(\d+)`)
	// "signatures=PackageSignatures{97e501d version:2, signatures:[dd80ed25], past signatures:[]}"
	signaturesRE = regexp.MustCompile(`signatures=PackageSignatures\{[^}]*?\bsignatures:\[([0-9a-f]+)`)
)

// Installed returns the installed companion's version name, or ok=false
// when the app is not installed.
func (c *Client) Installed(ctx context.Context) (version string, ok bool, err error) {
	v, ok, err := c.InstalledVersion(ctx)
	return v.Name, ok, err
}

// Version is a build of the app: the version name people read, the code
// Android compares, and the key it is signed with. Android installs an
// update only when it is signed with the installed build's key.
type Version struct {
	Name string
	Code int
	// Signer is Android's short name for the signing certificate, the hex
	// of its Java hash code, as `dumpsys package` prints it
	// ("signatures:[dd80ed25]"); "" when unknown.
	Signer string
}

// OldPublicSigner is the signing certificate of every build from 2026-11-07
// to 2026-11-08, android/companion/pinky-companion-debug.keystore, whose
// key and password are public since the repository is: anyone can sign an
// update of those builds. Pinky replaces them with a build signed with its
// private release key.
const OldPublicSigner = "dd80ed25"

// SignedElsewhere reports whether an installed build is signed with
// another key than an APK, so that the APK cannot install over it; false
// when either signer is unknown.
func (v Version) SignedElsewhere(apk Version) bool {
	return v.Signer != "" && apk.Signer != "" && v.Signer != apk.Signer
}

// InstalledVersion returns the installed build's version, and false when
// the app is not installed.
func (c *Client) InstalledVersion(ctx context.Context) (Version, bool, error) {
	out, err := c.run.Shell(ctx, "dumpsys package "+c.app.Package)
	if err != nil {
		return Version{}, false, err
	}
	v, ok := parsePackageVersion(out)
	return v, ok, nil
}

// parsePackageVersion reads `dumpsys package <pkg>` ("versionCode=68
// minSdk=29 targetSdk=36", "versionName=0.1.0", "signatures=..."); the
// first package entry wins, which is the installed one even while an
// update is staged. A missing package prints none of these lines.
func parsePackageVersion(out string) (Version, bool) {
	var v Version
	found := false
	if m := versionCodeRE.FindStringSubmatch(out); m != nil {
		v.Code, _ = strconv.Atoi(m[1])
		found = true
	}
	if m := versionNameRE.FindStringSubmatch(out); m != nil {
		v.Name = m[1]
		found = true
	}
	if m := signaturesRE.FindStringSubmatch(out); m != nil && found {
		v.Signer = m[1]
	}
	return v, found
}

// versionAsset is the file the build writes into the APK with its version,
// so it can be read without the Android SDK.
const versionAsset = "assets/pinky-companion.json"

// ErrNoVersion means an APK carries no version file: a build from before
// the version file was added (2026-11-07).
var ErrNoVersion = errors.New("the APK has no " + versionAsset)

// APKVersion reads which build a companion APK is and its version, from
// assets/pinky-companion.json, written by the same build as the manifest,
// and its signer from its signing block. An APK whose signing block cannot
// be read has no Signer.
func APKVersion(apk string) (App, Version, error) {
	z, err := zip.OpenReader(apk)
	if err != nil {
		return App{}, Version{}, fmt.Errorf("read %s: %w", apk, err)
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name != versionAsset {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return App{}, Version{}, err
		}
		defer r.Close()
		var v struct {
			Package     string `json:"package"`
			VersionName string `json:"version_name"`
			VersionCode int    `json:"version_code"`
		}
		if err := json.NewDecoder(io.LimitReader(r, 64<<10)).Decode(&v); err != nil {
			return App{}, Version{}, fmt.Errorf("%s in %s: %w", versionAsset, apk, err)
		}
		app, ok := AppFor(v.Package)
		if !ok || v.VersionCode <= 0 {
			return App{}, Version{}, fmt.Errorf("%s in %s names %q version %d, not Pinky's companion", versionAsset, apk, v.Package, v.VersionCode)
		}
		ver := Version{Name: v.VersionName, Code: v.VersionCode}
		if cert, err := APKCertificate(apk); err == nil {
			ver.Signer = SignerName(cert)
		}
		return app, ver, nil
	}
	return App{}, Version{}, fmt.Errorf("%s: %w", apk, ErrNoVersion)
}

// InstalledSHA256 hashes the installed base APK on the phone.
func (c *Client) InstalledSHA256(ctx context.Context) (string, error) {
	out, err := c.run.Shell(ctx, "pm path "+c.app.Package)
	if err != nil {
		return "", err
	}
	var path string
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), "package:"); ok && strings.HasSuffix(p, "/base.apk") {
			path = p
		}
	}
	if path == "" {
		return "", fmt.Errorf("%s is not installed", c.app.Package)
	}
	out, err = c.run.Shell(ctx, "sha256sum "+shellQuote(path))
	if err != nil {
		return "", err
	}
	sum, _, _ := strings.Cut(strings.TrimSpace(out), " ")
	if len(sum) != 64 {
		return "", fmt.Errorf("sha256sum: unexpected output %q", out)
	}
	return strings.ToLower(sum), nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
