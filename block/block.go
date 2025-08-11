package block

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"mmn/poh"
	"time"
)

type BlockStatus uint8

const (
	BlockPending BlockStatus = iota
	BlockFinalized
)

type Block struct {
	Slot      uint64
	PrevHash  [32]byte // hash of the last entry in the previous block
	Entries   []poh.Entry
	LeaderID  string
	Timestamp uint64
	Hash      [32]byte
	Signature []byte
	Status    BlockStatus
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
		Timestamp: uint64(time.Now().UnixNano()),
	}
	b.Hash = b.computeHash()
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
	binary.BigEndian.PutUint64(buf, b.Timestamp)
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
	sig := ed25519.Sign(privKey, b.Hash[:])
	b.Signature = sig
}

func (b *Block) VerifySignature(pubKey ed25519.PublicKey) bool {
	return ed25519.Verify(pubKey, b.Hash[:], b.Signature)
}

func (b *Block) VerifyPoH() error {
	return poh.VerifyEntries(b.PrevHash, b.Entries)
}

func (b *Block) LastEntryHash() [32]byte {
	return b.Entries[len(b.Entries)-1].Hash
}
