// File: pkg/blockchain/light.go
package blockchain

// LightBlockHeader contains only the essential fields of a block.
type LightBlockHeader struct {
	Index      int    `json:"index"`
	Timestamp  int64  `json:"timestamp"`
	PrevHash   string `json:"prev_hash"`
	Hash       string `json:"hash"`
	Difficulty int    `json:"difficulty"`
	Nonce      int    `json:"nonce"`
}

// ExtractHeaders returns the headers of all blocks in the blockchain.
func (bc *Blockchain) ExtractHeaders() []LightBlockHeader {
	headers := make([]LightBlockHeader, len(bc.Blocks))
	for i, blk := range bc.Blocks {
		headers[i] = LightBlockHeader{
			Index:      blk.Index,
			Timestamp:  blk.Timestamp,
			PrevHash:   blk.PrevHash,
			Hash:       blk.Hash,
			Difficulty: blk.Difficulty,
			Nonce:      blk.Nonce,
		}
	}
	return headers
}
