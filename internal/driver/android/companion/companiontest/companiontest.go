// Package companiontest writes companion APKs for tests: a zip with the
// version file Pinky reads and, when given a certificate, an APK Signing
// Block whose v2 signer holds it, laid out as apksigner does. The
// signature itself is not real; Pinky reads only the certificate.
package companiontest

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// APK writes an APK for the package with a version, signed with cert when
// it is not nil, and returns its path.
func APK(t testing.TB, pkg, versionName string, versionCode int, cert []byte) string {
	t.Helper()
	var zbuf bytes.Buffer
	z := zip.NewWriter(&zbuf)
	for name, body := range map[string]string{
		"AndroidManifest.xml":         "binary",
		"assets/pinky-companion.json": fmt.Sprintf(`{"package":%q,"version_name":%q,"version_code":%d}`+"\n", pkg, versionName, versionCode),
	} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	apk := zbuf.Bytes()
	if cert != nil {
		apk = sign(apk, cert)
	}
	p := filepath.Join(t.TempDir(), fmt.Sprintf("%s-%d.apk", pkg, versionCode))
	if err := os.WriteFile(p, apk, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// lp is a uint32-length-prefixed value.
func lp(parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	return append(binary.LittleEndian.AppendUint32(nil, uint32(len(body))), body...)
}

// sign puts a signing block with one v2 signer before the zip's central
// directory and moves the end of central directory's offset past it.
func sign(raw, cert []byte) []byte {
	eocd := bytes.LastIndex(raw, []byte{0x50, 0x4b, 0x05, 0x06})
	cd := binary.LittleEndian.Uint32(raw[eocd+16:])
	algo := binary.LittleEndian.AppendUint32(nil, 0x0103)
	signed := lp(lp(lp(algo, lp([]byte("digest")))), lp(lp(cert)), lp())
	signer := lp(signed, lp(lp(algo, lp([]byte("signature")))), lp([]byte("public key")))
	val := lp(signer)
	var pairs []byte
	pairs = binary.LittleEndian.AppendUint64(pairs, uint64(4+len(val)))
	pairs = binary.LittleEndian.AppendUint32(pairs, 0x7109871a)
	pairs = append(pairs, val...)
	size := uint64(len(pairs) + 24)
	var block []byte
	block = binary.LittleEndian.AppendUint64(block, size)
	block = append(block, pairs...)
	block = binary.LittleEndian.AppendUint64(block, size)
	block = append(block, "APK Sig Block 42"...)
	var out []byte
	out = append(out, raw[:cd]...)
	out = append(out, block...)
	out = append(out, raw[cd:]...)
	binary.LittleEndian.PutUint32(out[eocd+len(block)+16:], cd+uint32(len(block)))
	return out
}
