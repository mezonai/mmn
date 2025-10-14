package block

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/mezonai/mmn/common"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/poh"
	"github.com/pkg/errors"
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

func (b *BroadcastedBlock) VerifySignature() bool {

	pubKey, err := getPublicKeyFromLeaderID(b.LeaderID)
	if err != nil {
		logx.Error("BroadcastedBlock", fmt.Sprintf("Failed to get public key from LeaderID: %v", err))
		return false
	}

	if len(pubKey) != ed25519.PublicKeySize {
		logx.Warn("BLOCK", "verify block signature failure  different length of public key")
		return false
	}
	if len(b.Signature) != ed25519.SignatureSize {
		logx.Warn("BLOCK", "verify block signature failure different length of signature")
		return false
	}

	return ed25519.Verify(pubKey, b.Hash[:], b.Signature)
}

func getPublicKeyFromLeaderID(leaderID string) (ed25519.PublicKey, error) {
	if leaderID == "" {
		return nil, errors.New("leader ID cannot be empty")
	}

	pubKeyBytes, err := common.DecodeBase58ToBytes(leaderID)
	if err != nil {
		return nil, errors.Errorf("failed to decode leader ID: %w", err)
	}

	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return nil, errors.Errorf("invalid leader ID length: expected %d, got %d", ed25519.PublicKeySize, len(pubKeyBytes))
	}

	return ed25519.PublicKey(pubKeyBytes), nil
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
