package companion

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
)

// Reading which key an APK is signed with, without the Android SDK: the
// APK Signature Scheme v2 and v3 keep the signer's certificate in the APK
// Signing Block, which sits right before the zip's central directory.
// https://source.android.com/docs/security/features/apksigning/v2

const (
	sigBlockMagic = "APK Sig Block 42"
	sigV2ID       = 0x7109871a
	sigV3ID       = 0xf05368c0
	eocdSig       = 0x06054b50
	eocdSize      = 22
)

// ErrNotSigned means an APK has no v2 or v3 signature, such as one signed
// with the v1 (JAR) scheme alone, or not at all.
var ErrNotSigned = errors.New("the APK has no v2 or v3 signature")

// APKCertificate returns the DER certificate of an APK's first signer, the
// key Android checks an update against: from its v3 signature, else its
// v2 one.
func APKCertificate(apk string) ([]byte, error) {
	f, err := os.Open(apk)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	cert, err := readCertificate(f, fi.Size())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", apk, err)
	}
	return cert, nil
}

func readCertificate(r io.ReaderAt, size int64) ([]byte, error) {
	cd, err := centralDirectory(r, size)
	if err != nil {
		return nil, err
	}
	if cd < 32 {
		return nil, ErrNotSigned
	}
	footer := make([]byte, 24)
	if _, err := r.ReadAt(footer, cd-24); err != nil {
		return nil, err
	}
	if string(footer[8:]) != sigBlockMagic {
		return nil, ErrNotSigned
	}
	// The block's size counts everything but its first size field.
	n := binary.LittleEndian.Uint64(footer[:8])
	if n < 24 || n > uint64(cd-8) || n > 64<<20 {
		return nil, errors.New("bad APK signing block size")
	}
	block := make([]byte, n-24)
	if _, err := r.ReadAt(block, cd-int64(n)); err != nil {
		return nil, err
	}
	var v2, v3 []byte
	for len(block) > 0 {
		if len(block) < 12 {
			return nil, errors.New("bad APK signing block entry")
		}
		l := binary.LittleEndian.Uint64(block)
		if l < 4 || l > uint64(len(block)-8) {
			return nil, errors.New("bad APK signing block entry")
		}
		id := binary.LittleEndian.Uint32(block[8:])
		val := block[12 : 8+l]
		switch id {
		case sigV2ID:
			v2 = val
		case sigV3ID:
			v3 = val
		}
		block = block[8+l:]
	}
	val := v3
	if val == nil {
		val = v2
	}
	if val == nil {
		return nil, ErrNotSigned
	}
	return firstCertificate(val)
}

// firstCertificate reads signers[0].signed_data.certificates[0]: v2 and v3
// both start a signer with its signed data, and the signed data with the
// digests, then the certificates.
func firstCertificate(val []byte) ([]byte, error) {
	signers, _, err := prefixed(val)
	if err != nil {
		return nil, err
	}
	signer, _, err := prefixed(signers)
	if err != nil {
		return nil, err
	}
	signed, _, err := prefixed(signer)
	if err != nil {
		return nil, err
	}
	_, rest, err := prefixed(signed) // the digests
	if err != nil {
		return nil, err
	}
	certs, _, err := prefixed(rest)
	if err != nil {
		return nil, err
	}
	cert, _, err := prefixed(certs)
	if err != nil {
		return nil, err
	}
	if len(cert) == 0 {
		return nil, errors.New("the APK's signer has no certificate")
	}
	return cert, nil
}

// prefixed splits a uint32-length-prefixed value from the rest.
func prefixed(b []byte) (val, rest []byte, err error) {
	if len(b) < 4 {
		return nil, nil, errors.New("bad APK signature")
	}
	n := binary.LittleEndian.Uint32(b)
	if uint64(n) > uint64(len(b)-4) {
		return nil, nil, errors.New("bad APK signature")
	}
	return b[4 : 4+n], b[4+n:], nil
}

// centralDirectory returns the offset of the zip's central directory, from
// its end of central directory record.
func centralDirectory(r io.ReaderAt, size int64) (int64, error) {
	if size < eocdSize {
		return 0, errors.New("not a zip file")
	}
	tail := min(size, eocdSize+0xffff)
	buf := make([]byte, tail)
	if _, err := r.ReadAt(buf, size-tail); err != nil {
		return 0, err
	}
	for i := len(buf) - eocdSize; i >= 0; i-- {
		if binary.LittleEndian.Uint32(buf[i:]) != eocdSig {
			continue
		}
		if comment := int(binary.LittleEndian.Uint16(buf[i+20:])); comment != len(buf)-i-eocdSize {
			continue
		}
		cd := int64(binary.LittleEndian.Uint32(buf[i+16:]))
		if cd > size-tail+int64(i) {
			return 0, errors.New("bad zip central directory offset")
		}
		return cd, nil
	}
	return 0, errors.New("not a zip file")
}

// SignerName is Android's short name for a signing certificate, as
// `dumpsys package` prints it: the hex of Java's hash code of its DER
// bytes (android.content.pm.Signature.hashCode, Arrays.hashCode of signed
// bytes), without leading zeros.
func SignerName(cert []byte) string {
	var h int32 = 1
	for _, b := range cert {
		h = 31*h + int32(int8(b))
	}
	return strconv.FormatUint(uint64(uint32(h)), 16)
}
