	// Start mmn service
	mmnCfg := config.GetMmn()
	mainnetClient, _ := blockchain.NewGRPCClient(*mmnCfg)
	keystore, _ := keystore.NewPgEncryptedStore(db, mmnCfg.MasterKey)
	txService := service.NewTxService(mainnetClient, keystore, db)
	accountService := service.NewAccountService(mainnetClient, keystore, db)

type MmnConfig struct {
	Endpoint  string `yaml:"endpoint" json:"endpoint" usage:"mmn endpoint"`
	Timeout   int    `yaml:"timeout" json:"timeout" usage:"mmn timeout"`
	ChainID   string `yaml:"chain_id" json:"chain_id" usage:"mmn chain id"`
	MasterKey string `yaml:"master_key" json:"master_key" usage:"mmn master key"`
}	


func NewMmnConfig() *MmnConfig {
	return &MmnConfig{
		Endpoint:  "localhost:9001",
		Timeout:   10000,
		ChainID:   "1",
		MasterKey: "local-dev-master-key",
	}
}