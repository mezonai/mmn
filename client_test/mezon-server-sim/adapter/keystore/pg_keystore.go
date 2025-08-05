package keystore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"mezon/v2/mmn/domain"
	"mezon/v2/mmn/outbound"

	_ "github.com/lib/pq"
)

type pgStore struct {
	db   *sql.DB
	aead cipher.AEAD
}

func NewPgEncryptedStore(db *sql.DB, base64MasterKey string) (outbound.WalletManager, error) {
	mk, err := base64.StdEncoding.DecodeString(base64MasterKey)
	if err != nil {
		return nil, fmt.Errorf("master-key decode: %w", err)
	}
	if len(mk) != 32 {
		return nil, errors.New("master-key must be 32 bytes")
	}

	block, _ := aes.NewCipher(mk)
	aead, _ := cipher.NewGCM(block)

	return &pgStore{db: db, aead: aead}, nil
}

// ---------- helpers ----------
func (p *pgStore) encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return append(nonce, p.aead.Seal(nil, nonce, plain, nil)...), nil
}
func (p *pgStore) decrypt(ciphertext []byte) ([]byte, error) {
	ns := p.aead.NonceSize()
	if len(ciphertext) < ns {
		return nil, errors.New("ciphertext too short")
	}
	return p.aead.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
}

// ---------- WalletManager ----------
func (p *pgStore) LoadKey(uid uint64) (string, []byte, error) {
	var addr string
	var enc []byte

	// TODO: need use exist db model
	err := p.db.QueryRow(`SELECT address, enc_privkey FROM mmn_user_keys WHERE user_id=$1`, uid).
		Scan(&addr, &enc)
	if errors.Is(err, sql.ErrNoRows) {
		fmt.Printf("LoadKey ErrNoRows %d %s %s %v\n", uid, addr, enc, err)
		return "", nil, domain.ErrKeyNotFound
	}
	if err != nil {
		fmt.Printf("LoadKey Err %d %s %s %v\n", uid, addr, enc, err)
		return "", nil, err
	}

	priv, err := p.decrypt(enc)
	return addr, priv, err
}

func (p *pgStore) CreateKey(uid uint64) (string, error) {
	fmt.Printf("CreateKey start %d\n", uid)

	// Generate Ed25519 seed (32 bytes)
	seed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(seed)
	if err != nil {
		return "", err
	}

	// Generate Ed25519 key pair from seed
	privKey := ed25519.NewKeyFromSeed(seed)
	pubKey := privKey.Public().(ed25519.PublicKey)

	// Address is hex-encoded public key (for Verify to work)
	addr := hex.EncodeToString(pubKey)

	// Store the seed (not the full private key) for SignTx compatibility
	enc, err := p.encrypt(seed)
	if err != nil {
		return "", err
	}

	_, err = p.db.Exec(
		`INSERT INTO mmn_user_keys(user_id,address,enc_privkey) VALUES($1,$2,$3)`,
		uid, addr, enc,
	)
	fmt.Printf("CreateKey done %d %s\n", uid, addr)
	return addr, err
}
