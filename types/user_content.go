package types

type UserContent struct {
	Type              string   `json:"type"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	ImageCIDs         []string `json:"image_cids"`
	ParentHash        string   `json:"parent_hash"`
	RootHash          string   `json:"root_hash"`
	ReferenceTxHashes []string `json:"reference_tx_hashes"`
}
