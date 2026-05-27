//go:build !windows

package main

import "fmt"

func activateWindowsPackagedAppWithEnvironment(appUserModelID, arguments string, env []string) (uint32, error) {
	return 0, fmt.Errorf("Windows 打包应用激活只支持 Windows：%s %s", appUserModelID, arguments)
}

func waitForWindowsProcessID(processID uint32) error {
	return fmt.Errorf("Windows 进程等待只支持 Windows：%d", processID)
}

func terminateWindowsProcessID(processID uint32) error {
	return fmt.Errorf("Windows 进程终止只支持 Windows：%d", processID)
}
