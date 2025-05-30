package blockchain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"mmm/pkg/common"
	"mmm/pkg/crypto"
	"mmm/pkg/rlp"
	"time"
)

// Transaction represents a simple transaction.
type Transaction struct {
	Sender       string                 `json:"sender"`
	Recipient    string                 `json:"recipient"`
	Amount       float64                `json:"amount"`
	Timestamp    int64                  `json:"timestamp"`
	ContractName string                 `json:"contract_name,omitempty"`
	Method       string                 `json:"method,omitempty"`
	Params       map[string]interface{} `json:"params,omitempty"`
	Signature    string                 `json:"signature,omitempty"` // Digital signature (hex-encoded).
	Nonce        int                    `json:"nonce,omitempty"`     // Optional nonce to prevent replay.
	// In a more complete system, you might include digital signatures.
}

type TxRawTransaction struct {
	ChainID    *big.Int
	Nonce      uint64
	GasTipCap  *big.Int
	GasFeeCap  *big.Int
	Gas        uint64
	To         *common.Address
	Value      *big.Int
	Data       []byte
	AccessList []struct{}
	V, R, S    *big.Int
}

// extend methods for TxRawTransaction
func (tx *TxRawTransaction) GetChainID() *big.Int        { return tx.ChainID }
func (tx *TxRawTransaction) GetNonce() uint64            { return tx.Nonce }
func (tx *TxRawTransaction) GetMaxPriorityFee() *big.Int { return tx.GasTipCap }
func (tx *TxRawTransaction) GetMaxFeePerGas() *big.Int   { return tx.GasFeeCap }
func (tx *TxRawTransaction) GetGasLimit() uint64         { return tx.Gas }
func (tx *TxRawTransaction) GetTo() *common.Address      { return tx.To }
func (tx *TxRawTransaction) GetValue() *big.Int          { return tx.Value }
func (tx *TxRawTransaction) GetData() []byte             { return tx.Data }
func (tx *TxRawTransaction) GetAccessList() []struct{}   { return tx.AccessList }
func (tx *TxRawTransaction) GetV() *big.Int              { return tx.V }
func (tx *TxRawTransaction) GetR() *big.Int              { return tx.R }
func (tx *TxRawTransaction) GetS() *big.Int              { return tx.S }
func (tx *TxRawTransaction) GetTxType() byte             { return 0x02 } // EIP-1559

func (tx *TxRawTransaction) Sighash() (common.Hash, error) {
	payload := []interface{}{
		tx.ChainID, tx.Nonce, tx.GasTipCap, tx.GasFeeCap,
		tx.Gas, tx.To, tx.Value, tx.Data, tx.AccessList,
	}
	encoded, err := rlp.EncodeToBytes(payload)
	if err != nil {
		return common.Hash{}, err
	}
	return crypto.Keccak256Hash(append([]byte{0x02}, encoded...)), nil
}

func (tx *TxRawTransaction) SignatureBytes() ([]byte, error) {
	r := tx.R.FillBytes(make([]byte, 32))
	s := tx.S.FillBytes(make([]byte, 32))
	v := byte(tx.V.Uint64())
	return append(append(r, s...), v), nil
}

func (tx *TxRawTransaction) PublicKey() (string, error) {
	hash, err := tx.Sighash()
	if err != nil {
		return "", err
	}

	sig, err := tx.SignatureBytes()
	if err != nil {
		return "", err
	}

	pub, err := crypto.SigToPub(hash.Bytes(), sig)
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(crypto.FromECDSAPub(pub)), nil
}

func (tx *TxRawTransaction) From() (common.Address, error) {
	hash, err := tx.Sighash()
	if err != nil {
		return common.Address{}, err
	}

	sig, err := tx.SignatureBytes()
	if err != nil {
		return common.Address{}, err
	}

	pubKey, err := crypto.SigToPub(hash.Bytes(), sig)
	if err != nil {
		return common.Address{}, err
	}
	return crypto.PubkeyToAddress(*pubKey), nil
}

func (tx *TxRawTransaction) Hash() (common.Hash, error) {
	return tx.Sighash()
}

func (tx *TxRawTransaction) TxType() byte {
	return 0x02 // EIP-1559
}

func (tx *TxRawTransaction) IsContractCreation() bool {
	return tx.To == nil
}

func (tx *TxRawTransaction) DecodeFromRequest(hexTx string) error {
	// Remove 0x prefix
	if len(hexTx) >= 2 && hexTx[:2] == "0x" {
		hexTx = hexTx[2:]
	}

	rawBytes, err := hex.DecodeString(hexTx)
	if err != nil {
		return err
	}

	if rawBytes[0] != 0x02 {
		log.Fatalf("not an EIP-1559 tx, got type prefix: 0x%x", rawBytes[0])
	}
	// Check transaction type
	txType := rawBytes[0]
	if txType != 0x02 {
		log.Fatalf("Unsupported tx type 0x%x", txType)
	}

	// Remove the type prefix (0x02)
	rlpBytes := rawBytes[1:]
	err = rlp.DecodeBytes(rlpBytes, &tx)
	if err != nil {
		return err
	}

	return nil
}

func (tx *TxRawTransaction) Print() {
	fmt.Println("📦 Raw Transaction Info")

	hash, err := tx.Hash()
	if err == nil {
		fmt.Println("Hash:", hash.Hex())
	} else {
		fmt.Println("Hash: <error>", err)
	}

	from, err := tx.From()
	if err == nil {
		fmt.Println("From:", from.Hex())
	} else {
		fmt.Println("From: <error>", err)
	}

	to := "<contract creation>"
	if tx.To != nil {
		to = tx.To.Hex()
	}
	fmt.Println("To:", to)

	pubKey, err := tx.PublicKey()
	if err == nil {
		fmt.Println("PublicKey:", pubKey)
	} else {
		fmt.Println("PublicKey: <error>", err)
	}

	fmt.Println("Type:", "EIP-1559")
	fmt.Println("ChainID:", tx.ChainID.String())
	fmt.Println("Nonce:", tx.Nonce)
	fmt.Println("Value:", tx.Value.String())
	fmt.Println("GasLimit:", tx.Gas)
	fmt.Println("MaxFeePerGas:", tx.GasFeeCap.String())
	fmt.Println("MaxPriorityFeePerGas:", tx.GasTipCap.String())
	fmt.Printf("V: %s\n", tx.V.Text(16))
	fmt.Printf("R: %x\n", tx.R)
	fmt.Printf("S: %x\n", tx.S)
}

// end extend
// NewTransaction creates a new transaction and sets its timestamp.
func NewTransaction(sender, recipient string, amount float64, nonce int) *Transaction {
	return &Transaction{
		Sender:    sender,
		Recipient: recipient,
		Amount:    amount,
		Timestamp: time.Now().Unix(),
		Nonce:     nonce,
	}
}

// get
// String returns a string representation for signing.
func (tx *Transaction) String() string {
	return fmt.Sprintf("%s:%s:%f:%d:%d", tx.Sender, tx.Recipient, tx.Amount, tx.Timestamp, tx.Nonce)
}

// CalculateHash returns the SHA‑256 hash of the transaction.
func (tx *Transaction) CalculateHash() string {
	record := fmt.Sprintf("%s%s%f%d", tx.Sender, tx.Recipient, tx.Amount, tx.Timestamp)
	h := sha256.Sum256([]byte(record))
	return hex.EncodeToString(h[:])
}

// TransactionPool holds pending transactions.
type TransactionPool struct {
	Transactions []*Transaction
}

// AddTransaction appends a new transaction to the pool.
func (tp *TransactionPool) AddTransaction(tx *Transaction) {
	tp.Transactions = append(tp.Transactions, tx)
}

// Clear empties the transaction pool.
func (tp *TransactionPool) Clear() {
	tp.Transactions = []*Transaction{}
}
