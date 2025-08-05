package outbound

type WalletManager interface {
	LoadKey(userID uint64) (addr string, privKey []byte, err error)
	CreateKey(userID uint64) (addr string, err error)
}
