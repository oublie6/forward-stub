package receiver

import "strings"

type MatchKeyField struct {
	Name  string
	Value string
}

var matchKeyEscaper = strings.NewReplacer(
	`\\`, `\\\\`,
	`|`, `\|`,
	`=`, `\=`,
)

func BuildMatchKey(proto string, fields ...MatchKeyField) string {
	var b strings.Builder
	b.Grow(len(proto) + len(fields)*16)
	b.WriteString(proto)
	for _, field := range fields {
		b.WriteByte('|')
		b.WriteString(field.Name)
		b.WriteByte('=')
		b.WriteString(matchKeyEscaper.Replace(field.Value))
	}
	return b.String()
}
