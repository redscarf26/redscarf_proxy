package modernworld

import "strings"

// legacyChatItemLinks keeps the nine 3.3.5 item fields (through render level).
// Classic appends specialization and other client-only fields that AzerothCore
// rejects. Empty numeric fields are explicit zeros for the legacy parser.
// Addon payloads are opaque; escaped pipes and other link types are untouched.
func legacyChatItemLinks(text string, language uint32) string {
	if language == legacyLanguageAddon || language == modernLanguageAddon || language == modernLanguageLogged {
		return text
	}
	var out strings.Builder
	for len(text) > 0 {
		i := strings.IndexByte(text, '|')
		if i < 0 {
			out.WriteString(text)
			break
		}
		out.WriteString(text[:i])
		text = text[i:]
		if strings.HasPrefix(text, "||") {
			out.WriteString("||")
			text = text[2:]
			continue
		}
		if strings.HasPrefix(text, "|Hitem:") {
			end := strings.IndexByte(text[7:], '|')
			if end >= 0 {
				end += 7
				fields := strings.Split(text[7:end], ":")
				if strings.HasPrefix(text[end:], "|h[") && len(fields) >= 9 && fields[0] != "" {
					fields = fields[:9]
					for j := range fields {
						if fields[j] == "" {
							fields[j] = "0"
						}
					}
					out.WriteString("|Hitem:")
					out.WriteString(strings.Join(fields, ":"))
					text = text[end:]
					continue
				}
			}
		}
		out.WriteByte('|')
		text = text[1:]
	}
	return out.String()
}
