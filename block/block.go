package block

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"time"

	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/poh"
)

type BlockStatus uint8

const (
	BlockPending BlockStatus = iota
	BlockFinalized
)

type BlockCore struct {
	Slot       uint64
	PrevHash   [32]byte // hash of the last entry in the previous block
	LeaderID   string
	Timestamp  uint64 // unix nanos
	Hash       [32]byte
	Signature  []byte
	Status     BlockStatus
	InvalidPoH bool
	BankHash   [32]byte // hash of account state changes in this block
}

type Block struct {
	BlockCore
	Entries []poh.PersistentEntry
}

func (b *Block) LastEntryHash() [32]byte {
	if len(b.Entries) == 0 {
		logx.Warn("BLOCK", "LastEntryHash called on block with no entries, returning zero hash")
		return [32]byte{}
	}
	return b.Entries[len(b.Entries)-1].Hash
}

type BroadcastedBlock struct {
	BlockCore
	Entries []poh.Entry
}

// HashString returns the block hash as a hex string
func (b *BlockCore) HashString() string {
	return hex.EncodeToString(b.Hash[:])
}

// PrevHashString returns the previous block hash as a hex string
func (b *BlockCore) PrevHashString() string {
	return hex.EncodeToString(b.PrevHash[:])
}

func (b *BlockCore) CreationTimestamp() time.Time {
	return time.Unix(0, int64(b.Timestamp))
}

func AssembleBlock(
	slot uint64,
	prevHash [32]byte,
	leaderID string,
	entries []poh.Entry,
) *BroadcastedBlock {
	b := &BroadcastedBlock{
		BlockCore: BlockCore{
			Slot:      slot,
			PrevHash:  prevHash,
			LeaderID:  leaderID,
			Timestamp: uint64(time.Now().UnixNano()),
		},
		Entries: entries,
	}
	b.Hash = b.computeHash()
	return b
}

func (b *BroadcastedBlock) Sign(privKey ed25519.PrivateKey) {
	sig := ed25519.Sign(privKey, b.Hash[:])
	b.Signature = sig
}

func (b *BroadcastedBlock) computeHash() [32]byte {
	h := sha256.New()

	h.Write([]byte("BLOCK:"))
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
	// BankHash
	h.Write(b.BankHash[:])
	for _, e := range b.Entries {
		binary.BigEndian.PutUint64(buf, e.NumHashes)
		h.Write(buf)
		h.Write(e.Hash[:])
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func (b *BroadcastedBlock) VerifySignature(pubKey ed25519.PublicKey) bool {
	if len(pubKey) != ed25519.PublicKeySize {
		return false
	}
	if len(b.Signature) != ed25519.SignatureSize {
		return false
	}

	return ed25519.Verify(pubKey, b.Hash[:], b.Signature)
}

func (b *BroadcastedBlock) VerifyPoH() error {
	return poh.VerifyEntries(b.PrevHash, b.Entries, b.Slot)
}

func (b *BroadcastedBlock) LastEntryHash() [32]byte {
	if len(b.Entries) == 0 {
		logx.Warn("BLOCK", "LastEntryHash called on broadcasted block with no entries, returning zero hash")
		return [32]byte{}
	}
	return b.Entries[len(b.Entries)-1].Hash
}
