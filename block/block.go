package block

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"time"

	"mmn/poh"
)

type Block struct {
	Slot      uint64      // Slot number
	PrevHash  [32]byte    // Hash của block trước
	Entries   []poh.Entry // Các Entry (tx-entry + tick-only) của slot
	LeaderID  string      // ID của leader đã produce block này
	Timestamp time.Time   // Thời điểm assemble
	BlockHash [32]byte    // Hash của toàn bộ block (bỏ Signature)
	Signature []byte      // Signature của Leader
}

func AssembleBlock(
	slot uint64,
	prevHash [32]byte,
	leaderID string,
	entries []poh.Entry,
) *Block {
	b := &Block{
		Slot:      slot,
		PrevHash:  prevHash,
		Entries:   entries,
		LeaderID:  leaderID,
		Timestamp: time.Now(),
	}
	b.BlockHash = b.computeHash()
	return b
}

func (b *Block) computeHash() [32]byte {
	h := sha256.New()
	// Slot
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, b.Slot)
	h.Write(buf)
	// PrevHash
	h.Write(b.PrevHash[:])
	// LeaderID
	h.Write([]byte(b.LeaderID))
	// Timestamp (UnixNano)
	binary.BigEndian.PutUint64(buf, uint64(b.Timestamp.UnixNano()))
	h.Write(buf)
	for _, e := range b.Entries {
		binary.BigEndian.PutUint64(buf, e.NumHashes)
		h.Write(buf)
		h.Write(e.Hash[:])
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func (b *Block) Sign(privKey ed25519.PrivateKey) {
	sig := ed25519.Sign(privKey, b.BlockHash[:])
	b.Signature = sig
}

func (b *Block) VerifySignature(pubKey ed25519.PublicKey) bool {
	return ed25519.Verify(pubKey, b.BlockHash[:], b.Signature)
}
