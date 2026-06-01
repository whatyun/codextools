package main

import "strings"

func rootKeyString(contents, key string) string {
	for _, line := range strings.Split(contents, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			return ""
		}
		if strings.HasPrefix(trimmed, "#") || trimmed == "" {
			continue
		}
		left, right, ok := strings.Cut(trimmed, "=")
		if ok && strings.TrimSpace(left) == key {
			return unquoteToml(right)
		}
	}
	return ""
}

func upsertRootKey(contents, key, value string) string {
	lines := splitLines(contents)
	rootEnd := len(lines)
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			rootEnd = i
			break
		}
	}
	for i := 0; i < rootEnd; i++ {
		if rootLineKey(lines[i]) == key {
			lines[i] = key + " = " + value
			return ensureTrailingNewline(strings.Join(lines, "\n"))
		}
	}
	lines = append(lines[:rootEnd], append([]string{key + " = " + value}, lines[rootEnd:]...)...)
	return ensureTrailingNewline(strings.Join(lines, "\n"))
}

func removeRootKey(contents, key string) string {
	var lines []string
	inRoot := true
	for _, line := range splitLines(contents) {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			inRoot = false
		}
		if inRoot && rootLineKey(line) == key {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func rootLineKey(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "[") {
		return ""
	}
	left, _, ok := strings.Cut(trimmed, "=")
	if !ok {
		return ""
	}
	return strings.TrimSpace(left)
}

func tableValues(contents, table string) map[string]string {
	values := map[string]string{}
	header := "[" + table + "]"
	inTable := false
	for _, line := range splitLines(contents) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if inTable {
				break
			}
			inTable = trimmed == header
			continue
		}
		if !inTable || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		left, right, ok := strings.Cut(trimmed, "=")
		if ok {
			values[strings.TrimSpace(left)] = strings.TrimSpace(right)
		}
	}
	return values
}

func removeTable(contents, table string) string {
	header := "[" + table + "]"
	var lines []string
	skipping := false
	for _, line := range splitLines(contents) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if trimmed == header {
				skipping = true
				continue
			}
			skipping = false
		}
		if !skipping {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func upsertTomlTable(contents, table string, body []string) string {
	header := "[" + table + "]"
	lines := append([]string{header}, body...)
	return appendTomlBlock(removeTable(contents, table), lines)
}

func extractTomlTables(contents string, match func(table string) bool) string {
	var blocks [][]string
	lines := splitLines(contents)
	for i := 0; i < len(lines); {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "[") || !strings.HasSuffix(trimmed, "]") {
			i++
			continue
		}
		table := strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
		start := i
		i++
		for i < len(lines) {
			next := strings.TrimSpace(lines[i])
			if strings.HasPrefix(next, "[") && strings.HasSuffix(next, "]") {
				break
			}
			i++
		}
		if match(table) {
			block := append([]string{}, lines[start:i]...)
			for len(block) > 0 && strings.TrimSpace(block[len(block)-1]) == "" {
				block = block[:len(block)-1]
			}
			blocks = append(blocks, block)
		}
	}
	var out []string
	for index, block := range blocks {
		if index > 0 {
			out = append(out, "")
		}
		out = append(out, block...)
	}
	if len(out) == 0 {
		return ""
	}
	return ensureTrailingNewline(strings.Join(out, "\n"))
}

func removeTomlTablesMatching(contents string, match func(table string) bool) string {
	lines := splitLines(contents)
	var out []string
	for i := 0; i < len(lines); {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			table := strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
			start := i
			i++
			for i < len(lines) {
				next := strings.TrimSpace(lines[i])
				if strings.HasPrefix(next, "[") && strings.HasSuffix(next, "]") {
					break
				}
				i++
			}
			if match(table) {
				for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
					out = out[:len(out)-1]
				}
				continue
			}
			out = append(out, lines[start:i]...)
			continue
		}
		out = append(out, lines[i])
		i++
	}
	return ensureTrailingNewline(strings.TrimRight(strings.Join(out, "\n"), "\n"))
}

func unquoteToml(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, `"`)
	value = strings.TrimSuffix(value, `"`)
	return value
}

func quoteToml(value string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}

func tomlEscape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`)
}

func normalizeResponsesBaseURL(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" || baseURLHasPathAfterHost(trimmed) {
		return trimmed
	}
	return trimmed + "/v1"
}

func baseURLHasPathAfterHost(baseURL string) bool {
	after := baseURL
	if parts := strings.SplitN(baseURL, "://", 2); len(parts) == 2 {
		after = parts[1]
	}
	_, path, ok := strings.Cut(after, "/")
	return ok && strings.Trim(path, "/") != ""
}

func splitLines(contents string) []string {
	contents = strings.ReplaceAll(contents, "\r\n", "\n")
	if contents == "" {
		return []string{}
	}
	return strings.Split(strings.TrimSuffix(contents, "\n"), "\n")
}

func ensureTrailingNewline(value string) string {
	if !strings.HasSuffix(value, "\n") {
		return value + "\n"
	}
	return value
}
