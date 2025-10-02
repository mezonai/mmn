package consensus

import (
	"encoding/json"
	"fmt"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/mezonai/mmn/utils"
)

type CertType int

const (
	NOTAR_CERT CertType = iota
	NOTAR_FALLBACK_CERT
	SKIP_CERT
	FAST_FINAL_CERT
	FINAL_CERT
)

func (ct CertType) String() string {
	switch ct {
	case NOTAR_CERT:
		return "NOTAR_CERT"
	case NOTAR_FALLBACK_CERT:
		return "NOTAR_FALLBACK_CERT"
	case SKIP_CERT:
		return "SKIP_CERT"
	case FAST_FINAL_CERT:
		return "FAST_FINAL_CERT"
	case FINAL_CERT:
		return "FINAL_CERT"
	default:
		return "UNKNOWN_CERT"
	}
}

type Cert struct {
	Slot                 uint64
	CertType             CertType
	BlockHash            [32]byte
	Stake                uint64
	AggregateSig         []byte
	AggregateSigFallback []byte
	ListPubKeys          []string //TODO: optimize by store bitmap
	ListPubKeysFallback  []string //TODO: optimize by store bitmap
}

func (c *Cert) AggregateSignature(signatures []bls.Sign) {
	var sign bls.Sign
	sign.Aggregate(signatures)
	c.AggregateSig = sign.Serialize()
}

func (c *Cert) AggregateSignatureFallback(signatures []bls.Sign) {
	var sign bls.Sign
	sign.Aggregate(signatures)
	c.AggregateSigFallback = sign.Serialize()
}

func (c *Cert) VerifySignature() bool {
	switch c.CertType {
	case NOTAR_CERT, FAST_FINAL_CERT:
		return helperVerify(c.AggregateSig, c.ListPubKeys, c.createMessage(NOTAR_VOTE))

	case NOTAR_FALLBACK_CERT:
		valid := helperVerify(c.AggregateSig, c.ListPubKeys, c.createMessage(NOTAR_VOTE))
		if len(c.AggregateSigFallback) > 0 {
			valid = valid && helperVerify(c.AggregateSigFallback, c.ListPubKeysFallback, c.createMessage(NOTAR_FALLBACK_VOTE))
		}
		return valid
	case SKIP_CERT:
		valid := helperVerify(c.AggregateSig, c.ListPubKeys, c.createMessage(SKIP_VOTE))
		if len(c.AggregateSigFallback) > 0 {
			valid = valid && helperVerify(c.AggregateSigFallback, c.ListPubKeysFallback, c.createMessage(SKIP_FALLBACK_VOTE))
		}
		return valid

	case FINAL_CERT:
		return helperVerify(c.AggregateSig, c.ListPubKeys, c.createMessage(FINAL_VOTE))
	}
	return true
}

func (c *Cert) Validate() error {
	if c.Slot <= 0 {
		return fmt.Errorf("invalid slot number")
	}

	if len(c.AggregateSig) == 0 {
		return fmt.Errorf("missing aggregate signature")
	}

	return nil
}

func (c *Cert) createMessage(voteType VoteType) []byte {
	data, _ := json.Marshal(struct {
		Slot      uint64
		VoteType  VoteType
		BlockHash [32]byte
	}{
		Slot:      c.Slot,
		VoteType:  voteType,
		BlockHash: c.BlockHash,
	})
	return data
}

func helperVerify(sig []byte, pubkeys []string, msg []byte) bool {
	var sign bls.Sign
	if err := sign.Deserialize(sig); err != nil {
		return false
	}
	return sign.FastAggregateVerify(utils.StringToBlsPubkey(pubkeys), msg)
}
