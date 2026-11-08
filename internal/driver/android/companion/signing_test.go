package companion

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSignerNameOfTheOldPublicKey(t *testing.T) {
	cert, err := os.ReadFile(filepath.Join("testdata", "old-public-signer.der"))
	if err != nil {
		t.Fatal(err)
	}
	if got := SignerName(cert); got != OldPublicSigner {
		t.Errorf("SignerName = %q, want %q (dumpsys package of a build signed with it)", got, OldPublicSigner)
	}
	// Java's Integer.toHexString drops leading zeros, and so must we.
	if got := SignerName(nil); got != "1" {
		t.Errorf("SignerName(nil) = %q, want 1", got)
	}
}

// lp is a uint32-length-prefixed value.
func lp(parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	out := binary.LittleEndian.AppendUint32(nil, uint32(len(body)))
	return append(out, body...)
}

// signedAPK writes a zip with an APK Signing Block holding one signer per
// scheme, as apksigner lays it out.
func signedAPK(t *testing.T, v2cert, v3cert []byte) string {
	t.Helper()
	var zbuf bytes.Buffer
	z := zip.NewWriter(&zbuf)
	w, _ := z.Create("AndroidManifest.xml")
	w.Write([]byte("binary"))
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	raw := zbuf.Bytes()
	eocd := bytes.LastIndex(raw, []byte{0x50, 0x4b, 0x05, 0x06})
	cd := binary.LittleEndian.Uint32(raw[eocd+16:])

	signer := func(cert []byte, v3 bool) []byte {
		digests := lp(lp(binary.LittleEndian.AppendUint32(nil, 0x0103), lp([]byte("digest"))))
		signed := [][]byte{digests, lp(lp(cert))}
		if v3 {
			signed = append(signed, binary.LittleEndian.AppendUint32(nil, 28), binary.LittleEndian.AppendUint32(nil, 0x7fffffff))
		}
		signed = append(signed, lp())
		parts := [][]byte{lp(signed...)}
		if v3 {
			parts = append(parts, binary.LittleEndian.AppendUint32(nil, 28), binary.LittleEndian.AppendUint32(nil, 0x7fffffff))
		}
		parts = append(parts, lp(lp(binary.LittleEndian.AppendUint32(nil, 0x0103), lp([]byte("sig")))), lp([]byte("public key")))
		return lp(lp(parts...))
	}
	var pairs []byte
	pair := func(id uint32, val []byte) {
		pairs = binary.LittleEndian.AppendUint64(pairs, uint64(4+len(val)))
		pairs = binary.LittleEndian.AppendUint32(pairs, id)
		pairs = append(pairs, val...)
	}
	pair(0x42726577, make([]byte, 16)) // padding, as apksigner adds
	if v2cert != nil {
		pair(sigV2ID, signer(v2cert, false))
	}
	if v3cert != nil {
		pair(sigV3ID, signer(v3cert, true))
	}
	size := uint64(len(pairs) + 8 + 16)
	var block []byte
	block = binary.LittleEndian.AppendUint64(block, size)
	block = append(block, pairs...)
	block = binary.LittleEndian.AppendUint64(block, size)
	block = append(block, sigBlockMagic...)

	var apk []byte
	apk = append(apk, raw[:cd]...)
	apk = append(apk, block...)
	apk = append(apk, raw[cd:]...)
	binary.LittleEndian.PutUint32(apk[int(eocd)+len(block)+16:], cd+uint32(len(block)))
	p := filepath.Join(t.TempDir(), "signed.apk")
	if err := os.WriteFile(p, apk, 0o644); err != nil {
		t.Fatal(err)
	}
	// Still a zip that archive/zip reads.
	if r, err := zip.OpenReader(p); err != nil {
		t.Fatalf("the test APK is not a zip any more: %v", err)
	} else {
		r.Close()
	}
	return p
}

func TestAPKCertificate(t *testing.T) {
	v2, v3 := []byte("certificate v2"), []byte("certificate v3")
	got, err := APKCertificate(signedAPK(t, v2, v3))
	if err != nil || !bytes.Equal(got, v3) {
		t.Errorf("v2 and v3: %q, %v; want the v3 signer", got, err)
	}
	got, err = APKCertificate(signedAPK(t, v2, nil))
	if err != nil || !bytes.Equal(got, v2) {
		t.Errorf("v2 only: %q, %v", got, err)
	}
	unsigned := writeAPK(t, map[string]string{"AndroidManifest.xml": "binary"})
	if _, err := APKCertificate(unsigned); !errors.Is(err, ErrNotSigned) {
		t.Errorf("unsigned: %v, want ErrNotSigned", err)
	}
	notZip := filepath.Join(t.TempDir(), "x.apk")
	os.WriteFile(notZip, []byte("hello"), 0o644)
	if _, err := APKCertificate(notZip); err == nil {
		t.Error("a file that is not a zip gave no error")
	}
}

// TestAPKCertificateOfARealBuild reads the signer of a built APK, given in
// PINKY_COMPANION_APK, and compares it with PINKY_COMPANION_SIGNER when set
// (what `dumpsys package` prints for it once installed).
func TestAPKCertificateOfARealBuild(t *testing.T) {
	apk := os.Getenv("PINKY_COMPANION_APK")
	if apk == "" {
		t.Skip("PINKY_COMPANION_APK is not set")
	}
	app, v, err := APKVersion(apk)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %s %s (%d), signer %s", apk, app.Package, v.Name, v.Code, v.Signer)
	if want := os.Getenv("PINKY_COMPANION_SIGNER"); want != "" && v.Signer != want {
		t.Errorf("signer %q, want %q", v.Signer, want)
	}
	if v.Signer == "" {
		t.Error("no signer")
	}
}
