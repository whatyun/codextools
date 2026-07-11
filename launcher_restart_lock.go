package main

import (
	"os"
	"path/filepath"
)

var terminateWindowsRestartTargets = terminateWindowsTargetAppProcessesAndWait

func acquireLauncherRestartLock() (fileLockGuard, bool, error) {
	lockDir := filepath.Join(stateDir(), "locks")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, false, err
	}
	lock, err := tryAcquireFileLock(filepath.Join(lockDir, "launcher-restart.lock"))
	if err != nil {
		if isLoopbackGuardBusyError(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return lock, true, nil
}
