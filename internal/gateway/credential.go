package gateway

import "strings"

// credLine is one pasted account: a raw JWT, or email:pass:token.
type credLine struct {
	jwt   string
	label string // email; empty when the line is a bare JWT
}

// parseCredentialBlob splits a paste/import blob into accounts.
// Blank lines and '#' comments are dropped.
func parseCredentialBlob(blob string) []credLine {
	var out []credLine
	for _, ln := range strings.FieldsFunc(blob, func(r rune) bool { return r == '\n' || r == '\r' }) {
		if c, ok := parseCredentialLine(ln); ok {
			out = append(out, c)
		}
	}
	return out
}

// parseCredentialLine accepts either a raw JWT or email:pass:token.
// The token is the segment after the last ':', the email is before the
// first. A ':' inside the password stays in the password — a JWT has none,
// so SplitN(line, ":", 3) would glue that tail onto the token.
func parseCredentialLine(line string) (credLine, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return credLine{}, false
	}
	first := strings.Index(line, ":")
	last := strings.LastIndex(line, ":")
	if first > 0 && strings.Contains(line[:first], "@") {
		if last <= first {
			return credLine{}, false
		}
		email := strings.TrimSpace(line[:first])
		token := strings.TrimSpace(line[last+1:])
		if email == "" || token == "" {
			return credLine{}, false
		}
		return credLine{jwt: token, label: email}, true
	}
	return credLine{jwt: line}, true
}

func accountsFromCredentialText(text string) []map[string]any {
	creds := parseCredentialBlob(text)
	rows := make([]map[string]any, 0, len(creds))
	for _, c := range creds {
		rows = append(rows, map[string]any{"jwt": c.jwt, "label": c.label})
	}
	return rows
}
