//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const resolveWindowsPackageLaunchInfoScript = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$targetAumid = $env:CODEX_DESKTOP_AUMID
$targetFamily = $env:CODEX_DESKTOP_PACKAGE_FAMILY
$targetIdentity = $env:CODEX_DESKTOP_PACKAGE_IDENTITY
$package = Get-AppxPackage -Name $targetIdentity -ErrorAction SilentlyContinue |
  Where-Object { $_.PackageFamilyName -ieq $targetFamily } |
  Sort-Object Version -Descending |
  Select-Object -First 1
if ($null -eq $package) { exit 0 }
$manifest = Get-AppxPackageManifest -Package $package
$applications = @($manifest.Package.Applications.Application)
$matches = @($applications | Where-Object { ($package.PackageFamilyName + '!' + [string]$_.Id) -ieq $targetAumid })
if ($matches.Count -ne 1) { exit 0 }
$app = $matches[0]
$extensions = @($app.Extensions.Extension)
$aliasExtensions = @($extensions | Where-Object { [string]$_.Category -ieq 'windows.appExecutionAlias' })
$aliases = @()
foreach ($extension in $aliasExtensions) {
  foreach ($entry in @($extension.AppExecutionAlias.ExecutionAlias)) {
    $alias = [string]$entry.Alias
    if (-not [string]::IsNullOrWhiteSpace($alias)) { $aliases += $alias }
  }
}
$executable = [string]$app.Executable
$entryPoint = [string]$app.EntryPoint
if ([string]::IsNullOrWhiteSpace($executable)) {
  $extension = $aliasExtensions | Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_.Executable) } | Select-Object -First 1
  if ($null -ne $extension) { $executable = [string]$extension.Executable }
}
if ([string]::IsNullOrWhiteSpace($entryPoint)) {
  $extension = $aliasExtensions | Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_.EntryPoint) } | Select-Object -First 1
  if ($null -ne $extension) { $entryPoint = [string]$extension.EntryPoint }
}
$result = [PSCustomObject]@{
  appUserModelId = $package.PackageFamilyName + '!' + [string]$app.Id
  packageFamilyName = [string]$package.PackageFamilyName
  packageFullName = [string]$package.PackageFullName
  installLocation = [string]$package.InstallLocation
  applicationId = [string]$app.Id
  executable = $executable
  entryPoint = $entryPoint
  executionAliases = [string[]]$aliases
}
[Console]::Out.Write(($result | ConvertTo-Json -Compress -Depth 6))`

const (
	clsctxLocalServer   = 0x4
	activateOptionsNone = 0
	rpcEChangedMode     = syscall.Errno(0x80010106)
	processSynchronize  = 0x00100000
)

var (
	ole32                         = windows.NewLazySystemDLL("ole32.dll")
	kernel32                      = windows.NewLazySystemDLL("kernel32.dll")
	procCoCreateInstance          = ole32.NewProc("CoCreateInstance")
	procCoInitializeEx            = ole32.NewProc("CoInitializeEx")
	procCoUninitialize            = ole32.NewProc("CoUninitialize")
	procGetPackageFamilyName      = kernel32.NewProc("GetPackageFamilyName")
	clsidApplicationActivationMgr = windows.GUID{Data1: 0x45ba127d, Data2: 0x10a8, Data3: 0x46ea, Data4: [8]byte{0x8a, 0xb7, 0x56, 0xea, 0x90, 0x78, 0x94, 0x3c}}
	iidApplicationActivationMgr   = windows.GUID{Data1: 0x2e941141, Data2: 0x7f97, Data3: 0x4756, Data4: [8]byte{0xba, 0x1d, 0x9d, 0xec, 0xde, 0x89, 0x4a, 0x3d}}
)

func resolveWindowsRegisteredPackageLaunchInfo(appUserModelID string) (windowsRegisteredPackageLaunchInfo, bool) {
	reference := normalizeWindowsPackagedAppReference("aumid:" + strings.TrimSpace(appUserModelID))
	if reference == "" {
		return windowsRegisteredPackageLaunchInfo{}, false
	}
	canonicalAUMID := strings.TrimPrefix(reference, "aumid:")
	cacheKey := strings.ToLower(canonicalAUMID)
	if cached, found := windowsPackagedLaunchInfoCache.Load(cacheKey); found {
		if launchInfo, ok := cached.(windowsRegisteredPackageLaunchInfo); ok {
			return launchInfo, true
		}
	}
	packageFamily := windowsPackageFamilyFromAppUserModelID(canonicalAUMID)
	identity, _, _ := strings.Cut(packageFamily, "_")
	if packageFamily == "" || identity == "" {
		return windowsRegisteredPackageLaunchInfo{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", resolveWindowsPackageLaunchInfoScript)
	cmd.Env = append(os.Environ(),
		"CODEX_DESKTOP_AUMID="+canonicalAUMID,
		"CODEX_DESKTOP_PACKAGE_FAMILY="+packageFamily,
		"CODEX_DESKTOP_PACKAGE_IDENTITY="+identity,
	)
	hideSubprocessWindow(cmd)
	out, err := cmd.Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return windowsRegisteredPackageLaunchInfo{}, false
	}
	var raw windowsRegisteredPackageLaunchInfo
	if err := json.Unmarshal(out, &raw); err != nil {
		return windowsRegisteredPackageLaunchInfo{}, false
	}
	launchInfo, ok := normalizeWindowsRegisteredPackageLaunchInfo(raw, canonicalAUMID)
	if !ok {
		return windowsRegisteredPackageLaunchInfo{}, false
	}
	windowsPackagedLaunchInfoCache.Store(cacheKey, launchInfo)
	return launchInfo, true
}

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
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
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
	if hr == 0 || hr == 1 {
		return true, nil
	}
	err := syscall.Errno(hr)
	if err == rpcEChangedMode {
		return false, nil
	}
	return false, err
}

func windowsCDPPortOwnedByProcess(port uint16, processID uint32) bool {
	if port == 0 || processID == 0 {
		return false
	}
	for _, owner := range windowsCDPListenerProcessIDs(port) {
		if windowsProcessIsSameOrDescendant(owner, processID) {
			return true
		}
	}
	return false
}

func windowsCDPPortOwnedByPackagedProcess(port uint16, processID uint32, packageFamilyName string) bool {
	expectedFamily := strings.TrimSpace(packageFamilyName)
	if port == 0 || processID == 0 || expectedFamily == "" {
		return false
	}
	rootMatches := strings.EqualFold(windowsProcessPackageFamilyName(processID), expectedFamily)
	for _, owner := range windowsCDPListenerProcessIDs(port) {
		if !windowsProcessIsSameOrDescendant(owner, processID) {
			continue
		}
		if rootMatches || strings.EqualFold(windowsProcessPackageFamilyName(owner), expectedFamily) {
			return true
		}
	}
	return false
}

func windowsCDPPortOwnedByPackage(port uint16, packageFamilyName string) bool {
	expectedFamily := strings.TrimSpace(packageFamilyName)
	if port == 0 || expectedFamily == "" {
		return false
	}
	for _, owner := range windowsCDPListenerProcessIDs(port) {
		if strings.EqualFold(windowsProcessPackageFamilyName(owner), expectedFamily) {
			return true
		}
	}
	return false
}

func windowsCDPListenerProcessIDs(port uint16) []uint32 {
	if port == 0 {
		return nil
	}
	cmd := exec.Command("netstat", "-ano", "-p", "tcp")
	hideSubprocessWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var processIDs []uint32
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.EqualFold(fields[0], "TCP") {
			continue
		}
		separator := strings.LastIndex(fields[1], ":")
		if separator < 0 {
			continue
		}
		localPort, err := strconv.ParseUint(fields[1][separator+1:], 10, 16)
		if err != nil || uint16(localPort) != port {
			continue
		}
		owner, err := strconv.ParseUint(fields[len(fields)-1], 10, 32)
		if err == nil {
			processIDs = append(processIDs, uint32(owner))
		}
	}
	return processIDs
}

func windowsProcessPackageFamilyName(processID uint32) string {
	if processID == 0 {
		return ""
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(handle)
	var length uint32
	result, _, _ := procGetPackageFamilyName.Call(uintptr(handle), uintptr(unsafe.Pointer(&length)), 0)
	if syscall.Errno(result) != windows.ERROR_INSUFFICIENT_BUFFER || length == 0 {
		return ""
	}
	buffer := make([]uint16, length)
	result, _, _ = procGetPackageFamilyName.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&length)),
		uintptr(unsafe.Pointer(&buffer[0])),
	)
	if result != 0 {
		return ""
	}
	return windows.UTF16ToString(buffer)
}

func windowsTargetAppProcessIDs() []uint32 {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil
	}
	var processIDs []uint32
	for {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if isWindowsTargetAppExecutableName(name) {
			packageFamily := windowsProcessPackageFamilyName(entry.ProcessID)
			if isOpenAIDesktopPackageFamily(packageFamily) || (strings.EqualFold(name, "ChatGPT.exe") && packageFamily == "") {
				processIDs = append(processIDs, entry.ProcessID)
			}
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return processIDs
}

func windowsProcessIsSameOrDescendant(processID, ancestorID uint32) bool {
	if processID == 0 || ancestorID == 0 {
		return false
	}
	if processID == ancestorID {
		return true
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return false
	}
	parents := map[uint32]uint32{}
	for {
		parents[entry.ProcessID] = entry.ParentProcessID
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	current := processID
	for range 64 {
		parent, found := parents[current]
		if !found || parent == 0 || parent == current {
			return false
		}
		if parent == ancestorID {
			return true
		}
		current = parent
	}
	return false
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

func terminateWindowsProcessTree(processID uint32) error {
	if processID == 0 {
		return nil
	}
	cmd := exec.Command("taskkill", "/PID", strconv.FormatUint(uint64(processID), 10), "/T", "/F")
	hideSubprocessWindow(cmd)
	if err := cmd.Run(); err == nil {
		return nil
	}
	return terminateWindowsProcessID(processID)
}

func terminateWindowsTargetAppProcesses() {
	for _, processID := range windowsTargetAppProcessIDs() {
		_ = terminateWindowsProcessTree(processID)
	}
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
