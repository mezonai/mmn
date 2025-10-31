package validation

const (
	ValidationMaxTextDataLen  = 512
	ValidationMaxAmountLen    = 128
	ValidationMaxSignatureLen = 2048
	ValidationMaxTxHashLen    = 128
	ValidationMaxBlockRange   = 500
	ValidationMaxExtraInfoLen = 2048

	MaxBodyBytes     = 4 * 1024 * 1024
	MaxResponseBytes = 20 * 1024 * 1024
)

var InjectionPatterns = []string{
	"${{", "{{", "}}", "`", "$(", "|", "||", "&&", ">", "<script", "onerror=", "${jndi:", "..", "%0a", "%0d",
}
