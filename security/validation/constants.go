package validation

const (
	MaxShortTextLength = 128
	MaxLongTextLength  = 512

	MaxRequestBodySize  = 4 * 1024 * 1024  // 4 MB
	MaxResponseBodySize = 20 * 1024 * 1024 // 20 MB

	AllowedTextPattern = `^[A-Za-z0-9 !"#$%&'()*+,\-./:;=?@\[\\\]_{}|]+$` // Allowed characters in text fields (excepting <, >, ^, ~)
)

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
