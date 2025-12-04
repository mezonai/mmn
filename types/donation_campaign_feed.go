package types

type DonationCampaignFeed struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Cids        []string `json:"cids"`
	ParentHash  string   `json:"parent_hash"`
	RootHash    string   `json:"root_hash"`
}
