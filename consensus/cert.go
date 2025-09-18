package consensus

type CertType int

const (
	NOTAR_CERT CertType = iota
	NOTAR_FALLBACK_CERT
	SKIP_CERT
	FAST_FINAL_CERT
	FINAL_CERT
)

type Cert struct {
	Slot         uint64
	CertType     CertType
	BlockHash    [32]byte
	AggregateSig []byte
	Stake        uint64
}

//TODO: implement sign method - BLS
