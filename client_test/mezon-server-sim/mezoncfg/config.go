package mezoncfg

type MmnConfig struct {
	Endpoints string `yaml:"endpoint" json:"endpoint" usage:"mmn endpoint"`
	Timeout   int    `yaml:"timeout" json:"timeout" usage:"mmn timeout"`
	ChainID   string `yaml:"chain_id" json:"chain_id" usage:"mmn chain id"`
	MasterKey string `yaml:"master_key" json:"master_key" usage:"mmn master key"`
}

func NewMmnConfig() *MmnConfig {
	return &MmnConfig{
		Endpoints: "localhost:9001,localhost:9002,localhost:9003",
		Timeout:   10000,
		ChainID:   "1",
		MasterKey: "local-dev-master-key",
	}
}
