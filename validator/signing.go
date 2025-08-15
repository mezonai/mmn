package validator

import (
	"crypto/ed25519"
	"encoding/binary"

	"github.com/mezonai/mmn/types"
)

var sigDomain = []byte("MMN-SHRED-V1\x00") // domain-sep cố định

func sigBytes(sh *types.Shred) []byte {
	// dùng đúng độ dài payload thực
	payload := sh.Payload
	if int(sh.Header.PayloadSize) < len(payload) {
		payload = payload[:sh.Header.PayloadSize]
	}

	// kích thước: domain(16) + header(8+4+4+2+1+1+2=22) + payload
	out := make([]byte, 0, len(sigDomain)+22+len(payload))
	out = append(out, sigDomain...)

	// header (không gồm chữ ký)
	var u8 [8]byte
	var u4 [4]byte
	var u2 [2]byte

	binary.LittleEndian.PutUint64(u8[:], sh.Header.Slot)
	out = append(out, u8[:]...)

	binary.LittleEndian.PutUint32(u4[:], sh.Header.Index)
	out = append(out, u4[:]...)

	binary.LittleEndian.PutUint32(u4[:], sh.Header.FECSetIndex)
	out = append(out, u4[:]...)

	binary.LittleEndian.PutUint16(u2[:], sh.Header.Version)
	out = append(out, u2[:]...)

	out = append(out, byte(sh.Header.Type))
	out = append(out, sh.Header.Flags)

	binary.LittleEndian.PutUint16(u2[:], sh.Header.PayloadSize)
	out = append(out, u2[:]...)

	// payload thực
	out = append(out, payload...)
	return out
}

// Tạo hàm ký từ private key Ed25519
func NewSigner(priv ed25519.PrivateKey) func([]byte) [64]byte {
	return func(msg []byte) [64]byte {
		s := ed25519.Sign(priv, msg)
		var out [64]byte
		copy(out[:], s)
		return out
	}
}
