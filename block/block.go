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
	PrevHash  [32]byte    // Hash of previous block
	Entries   []poh.Entry // Entries of slot (tx-entry + tick-only)
	LeaderID  string      // ID of leader that produced this block
	Timestamp time.Time   // Time of assembly
	BlockHash [32]byte    // Hash of entire block (without Signature)
	Signature []byte      // Signature of Leader
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
