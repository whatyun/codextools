//go:build windows

package main

import (
	"sort"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const windowsUserEnvKey = `Environment`

func detectUserEnvConflicts() []envConflict {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsUserEnvKey, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()
	names, err := key.ReadValueNames(0)
	if err != nil {
		return nil
	}
	pairs := map[string]string{}
	for _, name := range names {
		value, _, err := key.GetStringValue(name)
		if err != nil {
			value = ""
		}
		pairs[name] = value
	}
	conflicts := detectedEnvConflictsFromPairs(pairs, "user")
	sortEnvConflicts(conflicts)
	return conflicts
}

func removeUserEnvValue(name string) (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsUserEnvKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return false, err
	}
	defer key.Close()
	names, err := key.ReadValueNames(0)
	if err != nil {
		return false, err
	}
	sort.Strings(names)
	found := false
	for _, existing := range names {
		if existing == name {
			found = true
			break
		}
	}
	if !found {
		return false, nil
	}
	if err := key.DeleteValue(name); err != nil && err != syscall.ERROR_FILE_NOT_FOUND {
		return false, err
	}
	return true, nil
}
