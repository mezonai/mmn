package block

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"sync"
	"time"

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
}

type Block struct {
	BlockCore
	Entries []poh.PersistentEntry
}

func (b *Block) LastEntryHash() [32]byte {
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

var (
	hasherPool = sync.Pool{New: func() interface{} { return sha256.New() }}
	buf8Pool   = sync.Pool{New: func() interface{} { return make([]byte, 8) }}
)

func (b *BroadcastedBlock) Sign(privKey ed25519.PrivateKey) {
	sig := ed25519.Sign(privKey, b.Hash[:])
	b.Signature = sig
}

func (b *BroadcastedBlock) computeHash() [32]byte {
	h := hasherPool.Get().(hash.Hash)
	h.Reset()
	defer hasherPool.Put(h)

	buf := buf8Pool.Get().([]byte)
	defer buf8Pool.Put(buf)

	// Slot
	binary.BigEndian.PutUint64(buf, b.Slot)
	_, _ = h.Write(buf)
	// PrevHash
	_, _ = h.Write(b.PrevHash[:])
	// LeaderID
	_, _ = h.Write([]byte(b.LeaderID))
	// Timestamp (UnixNano)
	binary.BigEndian.PutUint64(buf, b.Timestamp)
	_, _ = h.Write(buf)
	for _, e := range b.Entries {
		binary.BigEndian.PutUint64(buf, e.NumHashes)
		_, _ = h.Write(buf)
		_, _ = h.Write(e.Hash[:])
	}
	var out [32]byte
	sum := h.Sum(nil)
	copy(out[:], sum)
	return out
}

func (b *BroadcastedBlock) VerifySignature(pubKey ed25519.PublicKey) bool {
	return ed25519.Verify(pubKey, b.Hash[:], b.Signature)
}

func (b *BroadcastedBlock) VerifyPoH() error {
	return poh.VerifyEntries(b.PrevHash, b.Entries, b.Slot)
}

func (b *BroadcastedBlock) LastEntryHash() [32]byte {
	return b.Entries[len(b.Entries)-1].Hash
}
