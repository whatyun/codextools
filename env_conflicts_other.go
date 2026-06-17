//go:build !windows

package main

func detectUserEnvConflicts() []envConflict {
	return nil
}

func removeUserEnvValue(_ string) (bool, error) {
	return false, nil
}
