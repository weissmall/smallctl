package config

import "strings"

func Substitute(cmd string, defaults, overrides map[string]string) string {
	const dollarPlaceholder = "\x00DOLLAR\x00"

	result := strings.ReplaceAll(cmd, "$$", dollarPlaceholder)
	result = substituteVars(result, defaults, overrides)
	result = strings.ReplaceAll(result, dollarPlaceholder, "$")

	return result
}

func substituteVars(s string, defaults, overrides map[string]string) string {
	var buf strings.Builder
	buf.Grow(len(s))

	i := 0
	for i < len(s) {
		if i+2 < len(s) && s[i] == '$' && s[i+1] == '{' {
			close := strings.IndexByte(s[i+2:], '}')
			if close == -1 {
				buf.WriteByte(s[i])
				i++
				continue
			}
			close += i + 2

			varName := s[i+2 : close]
			val := resolveVar(varName, defaults, overrides)
			buf.WriteString(val)
			i = close + 1
		} else {
			buf.WriteByte(s[i])
			i++
		}
	}

	return buf.String()
}

func resolveVar(name string, defaults, overrides map[string]string) string {
	if v, ok := overrides[name]; ok {
		return v
	}
	if v, ok := defaults[name]; ok {
		return v
	}
	return ""
}
