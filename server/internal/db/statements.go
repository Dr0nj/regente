package db

import "strings"

// DDL versionado pode conter funções PG ($tag$) e triggers SQLite BEGIN/END.
// Separadores dentro de strings, comentários e corpos não encerram o comando.
func splitStatements(script string) []string {
	var out []string
	start, depth := 0, 0
	quote := byte(0)
	dollar := ""
	lineComment, blockComment := false, false
	create, trigger := false, false
	appendStatement := func(end int) {
		if p := strings.TrimSpace(script[start:end]); p != "" {
			out = append(out, p)
		}
		start = end + 1
		create = false
		trigger = false
		depth = 0
	}
	for i := 0; i < len(script); i++ {
		ch := script[i]
		if lineComment {
			if ch == '\n' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			if ch == '*' && i+1 < len(script) && script[i+1] == '/' {
				blockComment = false
				i++
			}
			continue
		}
		if dollar != "" {
			if strings.HasPrefix(script[i:], dollar) {
				i += len(dollar) - 1
				dollar = ""
			}
			continue
		}
		if quote != 0 {
			if ch == quote {
				if i+1 < len(script) && script[i+1] == quote {
					i++
				} else {
					quote = 0
				}
			}
			continue
		}
		if ch == '-' && i+1 < len(script) && script[i+1] == '-' {
			lineComment = true
			i++
			continue
		}
		if ch == '/' && i+1 < len(script) && script[i+1] == '*' {
			blockComment = true
			i++
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			quote = ch
			continue
		}
		if ch == '[' {
			quote = ']'
			continue
		}
		if ch == '$' {
			j := i + 1
			for j < len(script) && sqlWord(script[j]) {
				j++
			}
			if j < len(script) && script[j] == '$' {
				dollar = script[i : j+1]
				i = j
				continue
			}
		}
		if sqlWord(ch) {
			j := i + 1
			for j < len(script) && sqlWord(script[j]) {
				j++
			}
			word := strings.ToUpper(script[i:j])
			if word == "CREATE" {
				create = true
			}
			if create && word == "TRIGGER" {
				trigger = true
			}
			if trigger {
				if word == "BEGIN" || word == "CASE" {
					depth++
				}
				if word == "END" && depth > 0 {
					depth--
				}
			}
			i = j - 1
			continue
		}
		if ch == ';' && depth == 0 {
			appendStatement(i)
		}
	}
	if strings.TrimSpace(script[start:]) != "" {
		out = append(out, strings.TrimSpace(script[start:]))
	}
	return out
}
func sqlWord(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}
