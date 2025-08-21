// types/shred.go
package types

import (
	"encoding/binary"
	"fmt"
)

type ShredType uint8 // 0=data, 1=coding

type ShredHeader struct {
	Signature   [64]byte // Ed25519(header_without_sig || payload)
	Slot        uint64
	Index       uint32 // 0..N-1 in slot
	FECSetIndex uint32 // = Index / K (keep for future K adaptive)
	Version     uint16 // cluster/shred version
	Type        ShredType
	Flags       uint8  // bit0=end_of_slot
	PayloadSize uint16 // actual payload size (to cut padding)
}

type Shred struct {
	Header  ShredHeader
	Payload []byte // len <= MaxPayload, padding 0 if needed (RS requires fixed block size)
}

func (s *Shred) Encode() []byte {
	// Header size: 64 (sig) + 8 (slot) + 4 (index) + 4 (fec_set_index) + 2 (version) + 1 (type) + 1 (flags) + 2 (payload_size) = 86 bytes
	headerSize := 86
	totalSize := headerSize + len(s.Payload)
	buf := make([]byte, totalSize)

	offset := 0

	// Signature (64 bytes)
	copy(buf[offset:offset+64], s.Header.Signature[:])
	offset += 64

	// Slot (8 bytes)
	binary.LittleEndian.PutUint64(buf[offset:offset+8], s.Header.Slot)
	offset += 8

	// Index (4 bytes)
	binary.LittleEndian.PutUint32(buf[offset:offset+4], s.Header.Index)
	offset += 4

	// FECSetIndex (4 bytes)
	binary.LittleEndian.PutUint32(buf[offset:offset+4], s.Header.FECSetIndex)
	offset += 4

	// Version (2 bytes)
	binary.LittleEndian.PutUint16(buf[offset:offset+2], s.Header.Version)
	offset += 2

	// Type (1 byte)
	buf[offset] = byte(s.Header.Type)
	offset++

	// Flags (1 byte)
	buf[offset] = s.Header.Flags
	offset++

	// PayloadSize (2 bytes)
	binary.LittleEndian.PutUint16(buf[offset:offset+2], s.Header.PayloadSize)
	offset += 2

	// Payload
	copy(buf[offset:], s.Payload)

	return buf
}

func DecodeShred(b []byte) (Shred, error) {
	if len(b) < 86 {
		return Shred{}, fmt.Errorf("invalid shred: too short")
	}

	var shred Shred
	offset := 0

	// Signature (64 bytes)
	copy(shred.Header.Signature[:], b[offset:offset+64])
	offset += 64

	// Slot (8 bytes)
	shred.Header.Slot = binary.LittleEndian.Uint64(b[offset : offset+8])
	offset += 8

	// Index (4 bytes)
	shred.Header.Index = binary.LittleEndian.Uint32(b[offset : offset+4])
	offset += 4

	// FECSetIndex (4 bytes)
	shred.Header.FECSetIndex = binary.LittleEndian.Uint32(b[offset : offset+4])
	offset += 4

	// Version (2 bytes)
	shred.Header.Version = binary.LittleEndian.Uint16(b[offset : offset+2])
	offset += 2

	// Type (1 byte)
	shred.Header.Type = ShredType(b[offset])
	offset++

	// Flags (1 byte)
	shred.Header.Flags = b[offset]
	offset++

	// PayloadSize (2 bytes)
	shred.Header.PayloadSize = binary.LittleEndian.Uint16(b[offset : offset+2])
	offset += 2

	// Payload
	shred.Payload = make([]byte, len(b)-offset)
	copy(shred.Payload, b[offset:])

	return shred, nil
}

// Repair messages
type RepairReq struct {
	Slot        uint64
	FECSetIndex uint32
	Missing     []uint32 // missing indexes (data &/or coding)
}
type RepairResp struct {
	Shreds [][]byte // returned shred bytes
}
