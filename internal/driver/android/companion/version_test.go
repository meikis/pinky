package companion

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeAPK(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app.apk")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, body := range files {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return p
}

func TestAPKVersion(t *testing.T) {
	apk := writeAPK(t, map[string]string{
		"AndroidManifest.xml": "binary",
		versionAsset:          `{"package":"app.pinky.companion","version_name":"0.1.0","version_code":69}` + "\n",
	})
	app, v, err := APKVersion(apk)
	// Not signed: no signer.
	if err != nil || app != Release || v != (Version{Name: "0.1.0", Code: 69}) {
		t.Errorf("APKVersion = %+v, %+v, %v", app, v, err)
	}
	dev := writeAPK(t, map[string]string{versionAsset: `{"package":"app.pinky.companion.dev","version_name":"0.0.0-dev","version_code":470}`})
	if app, v, err := APKVersion(dev); err != nil || app != Dev || v.Code != 470 {
		t.Errorf("development build: %+v, %+v, %v", app, v, err)
	}
	old := writeAPK(t, map[string]string{"AndroidManifest.xml": "binary"})
	if _, _, err := APKVersion(old); !errors.Is(err, ErrNoVersion) {
		t.Errorf("APK without the version file: %v, want ErrNoVersion", err)
	}
	other := writeAPK(t, map[string]string{versionAsset: `{"package":"com.example","version_name":"1","version_code":5}`})
	if _, _, err := APKVersion(other); err == nil {
		t.Error("another package's version file was accepted")
	}
	if _, _, err := APKVersion(filepath.Join(t.TempDir(), "missing.apk")); err == nil {
		t.Error("a missing APK gave no error")
	}
}

func TestParsePackageVersion(t *testing.T) {
	out := "Packages:\n  Package [app.pinky.companion] (1a2b3c):\n    userId=10190\n    versionCode=68 minSdk=29 targetSdk=36\n    versionName=0.1.0\n"
	if v, ok := parsePackageVersion(out); !ok || v != (Version{Name: "0.1.0", Code: 68}) {
		t.Errorf("parsePackageVersion = %+v, %v", v, ok)
	}
	if v, ok := parsePackageVersion(""); ok {
		t.Errorf("not installed = %+v, %v", v, ok)
	}
	// As the Mi 10 Pro (Android 17) prints it for release 2026.1108.3.
	mi := "Packages:\n  Package [app.pinky.companion] (5e1a2f0):\n    versionCode=384 minSdk=29 targetSdk=36\n    versionName=2026.1108.3\n" +
		"    signatures=PackageSignatures{97e501d version:2, signatures:[dd80ed25], past signatures:[]}\n"
	v, ok := parsePackageVersion(mi)
	if !ok || v != (Version{Name: "2026.1108.3", Code: 384, Signer: OldPublicSigner}) {
		t.Errorf("parsePackageVersion = %+v, %v", v, ok)
	}
	if !v.SignedElsewhere(Version{Signer: "1f2e3d4c"}) || v.SignedElsewhere(Version{Signer: OldPublicSigner}) || v.SignedElsewhere(Version{}) {
		t.Error("SignedElsewhere is wrong")
	}
}
