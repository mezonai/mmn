package validation

import "github.com/mezonai/mmn/transaction"

const (
	MaxShortTextLength = 128
	MaxLongTextLength  = 5120

	DefaultRequestBodyLimit = 128 * 1024      // 128 KB
	LargeRequestBodyLimit   = 8 * 1024 * 1024 // 8 MB

	// Short text fields:
	SenderField    = "sender"
	RecipientField = "recipient"
	AmountField    = "amount"
	AddressField   = "address"
	TxHashField    = "tx_hash"

	// Long text fields:
	TextDataField  = "text_data"
	ExtraInfoField = "extra_info"
	ZkProofField   = "zk_proof"
	ZkPubField     = "zk_pub"
	SignatureField = "signature"

	// Max ReferenceTxHashes in UserContent transaction
	MaxReferenceTxs = 10
	ClientIPKey = "clientIP"
)

// LargeRequestMethods - set of gRPC methods that allow large request body
// Format: /<package>.<Service>/<Method>
var LargeRequestMethods = map[string]struct{}{
	"/mmn.BlockService/GetBlockByNumber": {},
}

var InjectionPatterns = []string{
	"${{", "{{", "}}", "${", "#{", "{%", "%}", "{{{", // templates/SSTI
	"%0a", "%0d", "%0a%0d", "%00", "%27", "%22", "%3c", "%3e", // encoded attacks (decode first)
	"${jndi:", "ldap://", "ldaps://", // JNDI/ldap
	"eval(", "exec(", "system(", "popen(", // dangerous funcs
}

// SkipSendEventTxTypes is a set of transaction types for which events should be skipped
var SkipSendEventTxTypes = map[int32]struct{}{
	transaction.TxTypeUserContent: {},
}

var TxExtraInfoTypeNeedValidateAddress = map[string]struct{}{
	transaction.TransactionExtraInfoDonationCampaignFeed: {},
}
