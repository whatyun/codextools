package main

func acquireLauncherSingleInstanceLock(debugPort uint16) (launcherSingleInstanceLock, bool, error) {
	guard, err := acquireResilientLoopbackPortGuard(launcherGuardPort)
	if err != nil {
		if isLoopbackGuardBusyError(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return guard, true, nil
}
