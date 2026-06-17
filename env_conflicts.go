package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type envConflict struct {
	Name         string `json:"name"`
	Source       string `json:"source"`
	ValuePresent bool   `json:"valuePresent"`
}

type envConflictRemoval struct {
	Name           string `json:"name"`
	RemovedProcess bool   `json:"removedProcess"`
	RemovedUser    bool   `json:"removedUser"`
}

func isCodexEnvConflictName(name string) bool {
	return strings.HasPrefix(strings.TrimSpace(name), "OPENAI_")
}

func detectedEnvConflictsFromPairs(pairs map[string]string, source string) []envConflict {
	conflicts := make([]envConflict, 0, len(pairs))
	for name, value := range pairs {
		name = strings.TrimSpace(name)
		if !isCodexEnvConflictName(name) {
			continue
		}
		conflicts = append(conflicts, envConflict{Name: name, Source: source, ValuePresent: strings.TrimSpace(value) != ""})
	}
	sortEnvConflicts(conflicts)
	return dedupeEnvConflicts(conflicts)
}

func detectEnvConflicts() []envConflict {
	processPairs := map[string]string{}
	for _, entry := range os.Environ() {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		processPairs[name] = value
	}
	conflicts := detectedEnvConflictsFromPairs(processPairs, "process")
	conflicts = append(conflicts, detectUserEnvConflicts()...)
	sortEnvConflicts(conflicts)
	return dedupeEnvConflicts(conflicts)
}

func removeEnvConflicts(names []string, backupDir string) (map[string]any, error) {
	names = normalizedEnvConflictNames(names)
	if len(names) == 0 {
		return map[string]any{"removed": []envConflictRemoval{}, "backupPath": nil}, nil
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return nil, err
	}
	before := filterEnvConflictsByName(detectEnvConflicts(), names)
	backupPath := filepath.Join(backupDir, "env-conflicts-"+time.Now().Format("20060102-150405.000")+".json")
	if err := atomicWriteJSON(backupPath, before); err != nil {
		return nil, err
	}
	removed := make([]envConflictRemoval, 0, len(names))
	for _, name := range names {
		_, hadProcess := os.LookupEnv(name)
		_ = os.Unsetenv(name)
		removedUser, err := removeUserEnvValue(name)
		if err != nil {
			return nil, err
		}
		removed = append(removed, envConflictRemoval{Name: name, RemovedProcess: hadProcess, RemovedUser: removedUser})
	}
	return map[string]any{"removed": removed, "backupPath": backupPath}, nil
}

func normalizedEnvConflictNames(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if isCodexEnvConflictName(name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return uniqueStrings(out)
}

func filterEnvConflictsByName(conflicts []envConflict, names []string) []envConflict {
	allowed := map[string]bool{}
	for _, name := range names {
		allowed[name] = true
	}
	out := make([]envConflict, 0, len(conflicts))
	for _, conflict := range conflicts {
		if allowed[conflict.Name] {
			out = append(out, conflict)
		}
	}
	return out
}

func sortEnvConflicts(conflicts []envConflict) {
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Name != conflicts[j].Name {
			return conflicts[i].Name < conflicts[j].Name
		}
		return envConflictSourceOrder(conflicts[i].Source) < envConflictSourceOrder(conflicts[j].Source)
	})
}

func dedupeEnvConflicts(conflicts []envConflict) []envConflict {
	out := conflicts[:0]
	var lastName, lastSource string
	for _, conflict := range conflicts {
		if conflict.Name == lastName && conflict.Source == lastSource {
			continue
		}
		out = append(out, conflict)
		lastName = conflict.Name
		lastSource = conflict.Source
	}
	return out
}

func envConflictSourceOrder(source string) int {
	if source == "process" {
		return 0
	}
	return 1
}
