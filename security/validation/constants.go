package validation

const (
	MaxShortTextLength = 128
	MaxLongTextLength  = 512

	DefaultRequestBodyLimit = 128 * 1024      // 128 KB
	LargeRequestBodyLimit   = 8 * 1024 * 1024 // 8 MB

	AllowedTextPattern = `^[A-Za-z0-9 !"#$%&'()*+,\-./:;=?@\[\\\]_{}|]+$` // Allowed characters in text fields (excepting <, >, ^, ~)

	// Short text fields:
	SenderField    = "sender"
	RecipientField = "recipient"
	AmountField    = "amount"
	AddressField   = "address"

	// Long text fields:
	TextDataField  = "text_data"
	ExtraInfoField = "extra_info"
	ZkProofField   = "zk_proof"
	ZkPubField     = "zk_pub"
	SignatureField = "signature"
)

// Methods that are allowed to have large request bodies
// Format: /<package>.<Service>/<Method>
var LargeRequestMethods = map[string]struct{}{
	"/mmn.BlockService/GetBlockByNumber": {},
}

var InjectionPatterns = []string{
	"${{", "{{", "}}", "${", "#{", "{%", "%}", "{{{", // templates/SSTI
	"$(", "||", "&&", ";", "--", "/*", "*/", // shell / sql comment / separators
	"'", "\"", // quotes
	"../", "..\\", "..", // path traversal
	"%0a", "%0d", "%0a%0d", "%00", "%27", "%22", "%3c", "%3e", // encoded attacks (decode first)
	"${jndi:", "ldap://", "ldaps://", // JNDI/ldap
	"eval(", "exec(", "system(", "popen(", // dangerous funcs
	"UNION", "SELECT", "INSERT", "UPDATE", "DELETE", "DROP", "EXEC", "xp_", // SQL
}
