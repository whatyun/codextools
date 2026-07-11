//go:build !windows

package main

import (
	"fmt"
	"time"
)

func resolveWindowsRegisteredPackageLaunchInfo(appUserModelID string) (windowsRegisteredPackageLaunchInfo, bool) {
	return windowsRegisteredPackageLaunchInfo{}, false
}

func activateWindowsPackagedAppWithEnvironment(appUserModelID, arguments string, env []string) (uint32, error) {
	return 0, fmt.Errorf("Windows 打包应用激活只支持 Windows：%s %s", appUserModelID, arguments)
}

func waitForWindowsProcessID(processID uint32) error {
	return fmt.Errorf("Windows 进程等待只支持 Windows：%d", processID)
}

func terminateWindowsProcessID(processID uint32) error {
	return fmt.Errorf("Windows 进程终止只支持 Windows：%d", processID)
}

func windowsCDPPortOwnedByProcess(port uint16, processID uint32) bool {
	return false
}

func windowsCDPPortOwnedByPackagedProcess(port uint16, processID uint32, packageFamilyName string) bool {
	return false
}

func windowsCDPPortOwnedByPackage(port uint16, packageFamilyName string) bool {
	return false
}

func windowsCDPPortOwnedByTargetApp(port uint16, appPath string) bool {
	return false
}

func windowsTCPListenerStatus(port uint16) ([]uint32, bool) {
	return nil, false
}

func windowsTCPPortStatus(port uint16) ([]uint32, []uint32, bool, bool) {
	return nil, nil, false, false
}

func windowsTargetAppProcessIDs() []uint32 {
	return nil
}

func terminateWindowsProcessTree(processID uint32) error {
	return fmt.Errorf("Windows 进程树终止只支持 Windows：%d", processID)
}

func terminateWindowsTargetAppProcesses() {}

func terminateWindowsTargetAppProcessesAndWait(appPath string, debugPort uint16, timeout time.Duration) ([]uint32, error) {
	return nil, nil
}
