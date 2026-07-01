package main

import (
	"encoding/json"
	"strconv"
	"strings"
)

type modelCatalogEntry struct {
	Slug        string
	DisplayName string
	Window      uint64
}

func parseModelSuffix(raw string) (string, uint64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasSuffix(raw, "]") {
		return raw, 0, false
	}
	open := strings.LastIndex(raw, "[")
	if open <= 0 {
		return raw, 0, false
	}
	slug := strings.TrimSpace(raw[:open])
	if slug == "" {
		return raw, 0, false
	}
	window, ok := parseModelWindowToken(raw[open+1 : len(raw)-1])
	if !ok {
		return raw, 0, false
	}
	return slug, window, true
}

func parseModelWindowToken(token string) (uint64, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, false
	}
	multiplier := uint64(1)
	last := token[len(token)-1]
	switch last {
	case 'k', 'K':
		multiplier = 1000
		token = strings.TrimSpace(token[:len(token)-1])
	case 'm', 'M':
		multiplier = 1000000
		token = strings.TrimSpace(token[:len(token)-1])
	}
	value, err := strconv.ParseUint(token, 10, 64)
	if err != nil || value == 0 {
		return 0, false
	}
	return value * multiplier, true
}

func normalizeModelListAndWindows(modelList, modelWindows string) (string, string) {
	windows := parseModelWindows(modelWindows)
	var lines []string
	for _, raw := range splitModelList(modelList) {
		slug, window, ok := parseModelSuffix(raw)
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		lines = append(lines, slug)
		if ok {
			windows[slug] = strconv.FormatUint(window, 10)
		}
	}
	if len(lines) == 0 {
		return strings.TrimSpace(modelList), marshalModelWindows(windows)
	}
	return strings.Join(uniqueStrings(lines), "\n"), marshalModelWindows(windows)
}

func parseModelWindows(raw string) map[string]string {
	windows := map[string]string{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return windows
	}
	var object map[string]any
	if json.Unmarshal([]byte(raw), &object) == nil {
		for key, value := range object {
			model := strings.TrimSpace(key)
			if model == "" {
				continue
			}
			if window, ok := parseModelWindowToken(stringFromAny(value)); ok {
				windows[model] = strconv.FormatUint(window, 10)
			}
		}
		return windows
	}
	for _, line := range strings.FieldsFunc(raw, func(ch rune) bool { return ch == '\n' || ch == '\r' || ch == ',' }) {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			key, value, ok = strings.Cut(line, ":")
		}
		if !ok {
			continue
		}
		model := strings.TrimSpace(key)
		if model == "" {
			continue
		}
		if window, ok := parseModelWindowToken(value); ok {
			windows[model] = strconv.FormatUint(window, 10)
		}
	}
	return windows
}

func marshalModelWindows(windows map[string]string) string {
	if len(windows) == 0 {
		return ""
	}
	data, err := json.Marshal(windows)
	if err != nil {
		return ""
	}
	return string(data)
}

func collectModelCatalogEntries(modelList, modelWindows, currentModel string) []modelCatalogEntry {
	windowMap := parseModelWindows(modelWindows)
	seen := map[string]bool{}
	var list []modelCatalogEntry
	add := func(raw string, target *[]modelCatalogEntry) {
		slug, suffixWindow, suffixOK := parseModelSuffix(raw)
		slug = strings.TrimSpace(slug)
		if slug == "" || seen[slug] {
			return
		}
		seen[slug] = true
		window := uint64(0)
		if configured, ok := windowMap[slug]; ok {
			window, _ = parseModelWindowToken(configured)
		}
		if window == 0 && suffixOK {
			window = suffixWindow
		}
		*target = append(*target, modelCatalogEntry{Slug: slug, DisplayName: slug, Window: window})
	}
	if strings.TrimSpace(currentModel) != "" {
		add(currentModel, &list)
	}
	for _, raw := range splitModelList(modelList) {
		add(raw, &list)
	}
	return list
}
