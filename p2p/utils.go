package p2p

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/consensus"

	"github.com/libp2p/go-libp2p/core/crypto"
	ma "github.com/multiformats/go-multiaddr"
)

// UnmarshalEd25519PrivateKey converts ed25519.PrivateKey to libp2p crypto.PrivKey
func UnmarshalEd25519PrivateKey(privKey ed25519.PrivateKey) (crypto.PrivKey, error) {
	// libp2p's UnmarshalEd25519PrivateKey only accepts 64-byte or 96-byte keys
	// If we have a 32-byte seed, we need to generate the full 64-byte private key
	switch len(privKey) {
	case ed25519.SeedSize: // 32 bytes - this is a seed
		// Generate full private key from seed
		fullPrivKey := ed25519.NewKeyFromSeed(privKey)
		return crypto.UnmarshalEd25519PrivateKey(fullPrivKey)
	case ed25519.PrivateKeySize: // 64 bytes - this is already a full private key
		return crypto.UnmarshalEd25519PrivateKey(privKey)
	default:
		return nil, fmt.Errorf("invalid Ed25519 private key length: got %d, expected %d or %d", len(privKey), ed25519.SeedSize, ed25519.PrivateKeySize)
	}
}

func (ln *Libp2pNetwork) GetOwnAddress() string {
	addrs := ln.host.Addrs()
	if len(addrs) > 0 {
		return fmt.Sprintf("%s/p2p/%s", addrs[0].String(), ln.host.ID().String())
	}
	return ""
}

func (ln *Libp2pNetwork) c(msg BlockMessage) *block.Block {
	return &block.Block{
		Slot:      msg.Slot,
		LeaderID:  msg.LeaderID,
		Timestamp: uint64(msg.Timestamp.Second()),
	}
}

func (ln *Libp2pNetwork) ConvertMessageToVote(msg VoteMessage) *consensus.Vote {
	return &consensus.Vote{
		Slot:      msg.Slot,
		VoterID:   msg.VoterID,
		Signature: msg.Signature,
	}
}

func (ln *Libp2pNetwork) ConvertMessageToBlock(msg BlockMessage) *block.Block {
	return &block.Block{
		Slot:      msg.Slot,
		LeaderID:  msg.LeaderID,
		Timestamp: uint64(msg.Timestamp.Second()),
	}
}

func AddrStrings(addrs []ma.Multiaddr) []string {
	var strAddrs []string
	for _, addr := range addrs {
		strAddrs = append(strAddrs, addr.String())
	}
	return strAddrs
}

// GenerateSyncRequestID generates a unique ID for sync requests
func GenerateSyncRequestID() string {
	// Generate 8 random bytes
	randomBytes := make([]byte, 8)
	rand.Read(randomBytes)

	// Combine timestamp and random bytes
	timestamp := time.Now().UnixNano()
	return fmt.Sprintf("%d-%s", timestamp, hex.EncodeToString(randomBytes))
}

// CleanupExpiredRequests removes expired sync requests (older than 5 minutes)
func (ln *Libp2pNetwork) CleanupExpiredRequests() {
	ln.syncTrackerMu.Lock()
	defer ln.syncTrackerMu.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute)

	for requestID, tracker := range ln.syncRequests {
		if tracker.StartTime.Before(cutoff) {
			tracker.CloseRequest()
			delete(ln.syncRequests, requestID)
		}
	}
}
