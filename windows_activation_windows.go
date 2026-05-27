//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	clsctxLocalServer   = 0x4
	activateOptionsNone = 0
	rpcEChangedMode     = syscall.Errno(0x80010106)
	processSynchronize  = 0x00100000
)

var (
	ole32                         = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance          = ole32.NewProc("CoCreateInstance")
	procCoInitializeEx            = ole32.NewProc("CoInitializeEx")
	procCoUninitialize            = ole32.NewProc("CoUninitialize")
	clsidApplicationActivationMgr = windows.GUID{Data1: 0x45ba127d, Data2: 0x10a8, Data3: 0x46ea, Data4: [8]byte{0x8a, 0xb7, 0x56, 0xea, 0x90, 0x78, 0x94, 0x3c}}
	iidApplicationActivationMgr   = windows.GUID{Data1: 0x2e941141, Data2: 0x7f97, Data3: 0x4756, Data4: [8]byte{0xba, 0x1d, 0x9d, 0xec, 0xde, 0x89, 0x4a, 0x3d}}
)

type applicationActivationManager struct {
	lpVtbl *applicationActivationManagerVtbl
}

type applicationActivationManagerVtbl struct {
	queryInterface      uintptr
	addRef              uintptr
	release             uintptr
	activateApplication uintptr
	activateForFile     uintptr
	activateForProtocol uintptr
}

func activateWindowsPackagedAppWithEnvironment(appUserModelID, arguments string, env []string) (uint32, error) {
	previous := applyProxyEnvironment(env)
	processID, err := activateWindowsPackagedApp(appUserModelID, arguments)
	restoreProxyEnvironment(previous)
	return processID, err
}

func activateWindowsPackagedApp(appUserModelID, arguments string) (uint32, error) {
	initialized, err := coInitialize()
	if err != nil {
		return 0, err
	}
	if initialized {
		defer procCoUninitialize.Call()
	}

	var manager *applicationActivationManager
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidApplicationActivationMgr)),
		0,
		uintptr(clsctxLocalServer),
		uintptr(unsafe.Pointer(&iidApplicationActivationMgr)),
		uintptr(unsafe.Pointer(&manager)),
	)
	if hr != 0 {
		return 0, syscall.Errno(hr)
	}
	if manager == nil {
		return 0, errors.New("ApplicationActivationManager 不可用")
	}
	defer syscall.SyscallN(manager.lpVtbl.release, uintptr(unsafe.Pointer(manager)))

	appID, err := windows.UTF16PtrFromString(appUserModelID)
	if err != nil {
		return 0, err
	}
	args, err := windows.UTF16PtrFromString(arguments)
	if err != nil {
		return 0, err
	}
	var processID uint32
	hr, _, _ = syscall.SyscallN(
		manager.lpVtbl.activateApplication,
		uintptr(unsafe.Pointer(manager)),
		uintptr(unsafe.Pointer(appID)),
		uintptr(unsafe.Pointer(args)),
		uintptr(activateOptionsNone),
		uintptr(unsafe.Pointer(&processID)),
	)
	if hr != 0 {
		return 0, syscall.Errno(hr)
	}
	return processID, nil
}

func coInitialize() (bool, error) {
	hr, _, _ := procCoInitializeEx.Call(0, uintptr(windows.COINIT_APARTMENTTHREADED))
	if hr == 0 {
		return true, nil
	}
	err := syscall.Errno(hr)
	if err == rpcEChangedMode {
		return false, nil
	}
	return false, err
}

func waitForWindowsProcessID(processID uint32) error {
	if processID == 0 {
		return nil
	}
	handle, err := windows.OpenProcess(processSynchronize|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return fmt.Errorf("无法等待 Windows 进程 %d：%w", processID, err)
	}
	defer windows.CloseHandle(handle)
	_, err = windows.WaitForSingleObject(handle, windows.INFINITE)
	if err != nil {
		return fmt.Errorf("等待 Windows 进程 %d 失败：%w", processID, err)
	}
	return nil
}

func terminateWindowsProcessID(processID uint32) error {
	if processID == 0 {
		return nil
	}
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return fmt.Errorf("无法打开 Windows 进程 %d：%w", processID, err)
	}
	defer windows.CloseHandle(handle)
	if err := windows.TerminateProcess(handle, 1); err != nil {
		return fmt.Errorf("无法终止 Windows 进程 %d：%w", processID, err)
	}
	return nil
}

func applyProxyEnvironment(env []string) [3]savedEnvironmentValue {
	keys := [3]string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"}
	var previous [3]savedEnvironmentValue
	for i, key := range keys {
		current, exists := os.LookupEnv(key)
		previous[i] = savedEnvironmentValue{key: key, value: current, ok: exists}
		value, ok := lookupEnvFromList(env, key)
		if ok {
			_ = os.Setenv(key, value)
		}
	}
	return previous
}

func restoreProxyEnvironment(previous [3]savedEnvironmentValue) {
	for _, saved := range previous {
		if saved.ok {
			_ = os.Setenv(saved.key, saved.value)
		} else {
			_ = os.Unsetenv(saved.key)
		}
	}
}

type savedEnvironmentValue struct {
	key   string
	value string
	ok    bool
}

func lookupEnvFromList(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(strings.ToUpper(entry), prefix) {
			return entry[len(prefix):], true
		}
	}
	return "", false
}
