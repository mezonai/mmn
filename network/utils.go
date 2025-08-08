package network

import (
	"crypto/ed25519"
	"fmt"
	"mmn/block"
	"mmn/consensus"

	"github.com/libp2p/go-libp2p/core/crypto"
)

func UnmarshalEd25519PrivateKey(private ed25519.PrivateKey) (crypto.PrivKey, error) {
	if len(private) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid ed25519 private key length")
	}
	seed := private[:32]
	return crypto.UnmarshalEd25519PrivateKey(seed)
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
		Timestamp: msg.Timestamp,
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
		Timestamp: msg.Timestamp,
	}
}
