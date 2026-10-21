package sqlstore

import (
	"strconv"
	"strings"
)

// bind numbers placeholders in application-authored SQL. Queries use ? only
// for parameters, never PostgreSQL JSON operators, dollar quotes, or comments.
// Quoted SQL strings and identifiers are preserved. User data is always passed
// separately as arguments and never enters this function.
func (d Dialect) bind(query string) string {
	if d != PostgreSQL || !strings.Contains(query, "?") {
		return query
	}
	var b strings.Builder
	b.Grow(len(query))
	var quote byte
	n := 0
	for i := 0; i < len(query); i++ {
		c := query[i]
		if quote != 0 {
			b.WriteByte(c)
			if c == quote {
				if i+1 < len(query) && query[i+1] == quote {
					i++
					b.WriteByte(query[i])
				} else {
					quote = 0
				}
			}
		} else if c == '\'' || c == '"' {
			quote = c
			b.WriteByte(c)
		} else if c == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}
