//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

func windowsCDPPortOwnedByTargetApp(port uint16, appPath string) bool {
	spec, ok := windowsRestartTargetSpecForApp(appPath)
	if !ok {
		return false
	}
	spec = expandWindowsRestartTargetSpecForPort(spec, port)
	targetProcessIDs := windowsRestartTargetProcessIDs(spec)
	for _, owner := range windowsCDPListenerProcessIDs(port) {
		if !windowsProcessInSession(owner, spec.sessionID) {
			continue
		}
		if windowsProcessBelongsToRestartPackage(owner, spec) || windowsProcessMatchesRestartTarget(owner, spec) {
			return true
		}
		for _, targetProcessID := range targetProcessIDs {
			if windowsProcessIsSameOrDescendant(owner, targetProcessID) {
				return true
			}
		}
	}
	return false
}

func windowsCDPListenerProcessIDs(port uint16) []uint32 {
	processIDs, _ := windowsTCPListenerStatus(port)
	return processIDs
}

func windowsTCPListenerStatus(port uint16) ([]uint32, bool) {
	listeners, _, listenerKnown, _ := windowsTCPPortStatus(port)
	return listeners, listenerKnown
}

func windowsTCPPortStatus(port uint16) ([]uint32, []uint32, bool, bool) {
	if port == 0 {
		return nil, nil, true, true
	}
	if out, err := runWindowsNetstat("-ano", "-q", "-p", "tcp"); err == nil {
		status := parseWindowsTCPPortProcessIDs(string(out), port)
		return status.Listening, status.Bound, true, true
	}
	out, err := runWindowsNetstat("-ano", "-p", "tcp")
	if err != nil {
		return nil, nil, false, false
	}
	return parseWindowsTCPListenerProcessIDs(string(out), port), nil, true, false
}

func runWindowsNetstat(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "netstat", args...)
	hideSubprocessWindow(cmd)
	return cmd.Output()
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

type windowsRestartTargetSpec struct {
	packageFamilies []string
	executablePath  string
	sessionID       uint32
}

func windowsRestartTargetSpecForApp(appPath string) (windowsRestartTargetSpec, bool) {
	var sessionID uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &sessionID); err != nil {
		return windowsRestartTargetSpec{}, false
	}
	if reference := normalizeWindowsPackagedAppReference(appPath); reference != "" {
		appUserModelID := strings.TrimPrefix(reference, "aumid:")
		if family := windowsPackageFamilyFromAppUserModelID(appUserModelID); family != "" {
			return windowsRestartTargetSpec{
				packageFamilies: []string{family},
				executablePath:  windowsPackagedDirectExecutable(appUserModelID),
				sessionID:       sessionID,
			}, true
		}
	}
	if appUserModelID := windowsAppUserModelIDFromPackagePath(appPath); appUserModelID != "" {
		if family := windowsPackageFamilyFromAppUserModelID(appUserModelID); family != "" {
			executable := strings.TrimSpace(buildCodexExecutable(appPath))
			if resolved, err := filepath.EvalSymlinks(executable); err == nil {
				executable = resolved
			}
			return windowsRestartTargetSpec{packageFamilies: []string{family}, executablePath: executable, sessionID: sessionID}, true
		}
	}
	executable := strings.TrimSpace(buildCodexExecutable(appPath))
	if executable == "" {
		return windowsRestartTargetSpec{}, false
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	return windowsRestartTargetSpec{executablePath: executable, sessionID: sessionID}, true
}

func expandWindowsRestartTargetSpecForPort(spec windowsRestartTargetSpec, port uint16) windowsRestartTargetSpec {
	listeners, bound, _, _ := windowsTCPPortStatus(port)
	owners := append(append([]uint32(nil), listeners...), bound...)
	if len(owners) == 0 {
		return spec
	}
	officialRoots := windowsOfficialPackagedTargetProcesses(spec.sessionID)
	for _, owner := range owners {
		if !windowsProcessInSession(owner, spec.sessionID) {
			continue
		}
		if family := windowsProcessPackageFamilyName(owner); isOpenAIDesktopPackageFamily(family) {
			spec.packageFamilies = appendUniqueFold(spec.packageFamilies, family)
			continue
		}
		for processID, family := range officialRoots {
			if windowsProcessIsSameOrDescendant(owner, processID) {
				spec.packageFamilies = appendUniqueFold(spec.packageFamilies, family)
			}
		}
	}
	return spec
}

func windowsOfficialPackagedTargetProcesses(sessionID uint32) map[uint32]string {
	result := map[uint32]string{}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return result
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return result
	}
	for {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if isWindowsTargetAppExecutableName(name) && windowsProcessInSession(entry.ProcessID, sessionID) {
			if family := windowsProcessPackageFamilyName(entry.ProcessID); isOpenAIDesktopPackageFamily(family) {
				result[entry.ProcessID] = family
			}
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return result
}

func windowsRestartTargetProcessIDs(spec windowsRestartTargetSpec) []uint32 {
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
		if windowsProcessMatchesRestartTargetWithName(entry.ProcessID, name, spec) {
			processIDs = append(processIDs, entry.ProcessID)
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return processIDs
}

func windowsProcessMatchesRestartTarget(processID uint32, spec windowsRestartTargetSpec) bool {
	if processID == 0 || !windowsProcessInSession(processID, spec.sessionID) {
		return false
	}
	imagePath := windowsProcessExecutablePath(processID)
	if imagePath == "" || !isWindowsTargetAppExecutableName(filepath.Base(imagePath)) {
		return false
	}
	return windowsProcessMatchesRestartTargetWithName(processID, filepath.Base(imagePath), spec)
}

func windowsProcessBelongsToRestartPackage(processID uint32, spec windowsRestartTargetSpec) bool {
	if processID == 0 || !windowsProcessInSession(processID, spec.sessionID) {
		return false
	}
	packageFamily := windowsProcessPackageFamilyName(processID)
	for _, expectedFamily := range spec.packageFamilies {
		if strings.EqualFold(packageFamily, expectedFamily) {
			return true
		}
	}
	return false
}

func windowsProcessMatchesRestartTargetWithName(processID uint32, name string, spec windowsRestartTargetSpec) bool {
	if processID == 0 || !isWindowsTargetAppExecutableName(name) || !windowsProcessInSession(processID, spec.sessionID) {
		return false
	}
	packageFamily := windowsProcessPackageFamilyName(processID)
	imagePath := windowsProcessExecutablePath(processID)
	for _, expectedFamily := range spec.packageFamilies {
		if windowsRestartTargetProcessMatches(name, packageFamily, imagePath, expectedFamily, spec.executablePath) {
			return true
		}
	}
	return len(spec.packageFamilies) == 0 && windowsRestartTargetProcessMatches(name, packageFamily, imagePath, "", spec.executablePath)
}

func windowsProcessInSession(processID, sessionID uint32) bool {
	if processID == 0 {
		return false
	}
	var processSessionID uint32
	return windows.ProcessIdToSessionId(processID, &processSessionID) == nil && processSessionID == sessionID
}

func windowsProcessExecutablePath(processID uint32) string {
	if processID == 0 {
		return ""
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil || size == 0 {
		return ""
	}
	return windows.UTF16ToString(buffer[:size])
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

func terminateWindowsTargetAppProcessesAndWait(appPath string, debugPort uint16, timeout time.Duration) ([]uint32, error) {
	spec, ok := windowsRestartTargetSpecForApp(appPath)
	if !ok {
		return nil, errors.New("无法安全确认要重启的 Windows ChatGPT/Codex 进程身份")
	}
	spec = expandWindowsRestartTargetSpecForPort(spec, debugPort)
	processIDs := windowsRestartTargetProcessIDs(spec)
	if len(processIDs) == 0 {
		return nil, nil
	}
	deadline := time.Now().Add(timeout)
	graceDeadline := time.Now().Add(1500 * time.Millisecond)
	if graceDeadline.After(deadline) {
		graceDeadline = deadline
	}
	for time.Now().Before(graceDeadline) {
		if len(runningWindowsRestartTargetProcessIDs(processIDs, spec)) == 0 {
			return processIDs, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	var terminationErrors []string
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	for _, processID := range runningWindowsRestartTargetProcessIDs(processIDs, spec) {
		if !windowsProcessMatchesRestartTarget(processID, spec) {
			continue
		}
		if err := terminateWindowsProcessTreeContext(ctx, processID); err != nil {
			terminationErrors = append(terminationErrors, fmt.Sprintf("PID %d: %v", processID, err))
		}
	}
	for {
		remaining := runningWindowsRestartTargetProcessIDs(processIDs, spec)
		if len(remaining) == 0 {
			return processIDs, nil
		}
		if time.Now().After(deadline) {
			message := fmt.Sprintf("旧 ChatGPT/Codex 进程未退出：%v", remaining)
			if len(terminationErrors) > 0 {
				message += "；终止错误：" + strings.Join(terminationErrors, "；")
			}
			return processIDs, errors.New(message)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func terminateWindowsProcessTreeContext(ctx context.Context, processID uint32) error {
	if processID == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "taskkill", "/PID", strconv.FormatUint(uint64(processID), 10), "/T", "/F")
	hideSubprocessWindow(cmd)
	if err := cmd.Run(); err == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return terminateWindowsProcessID(processID)
}

func runningWindowsRestartTargetProcessIDs(processIDs []uint32, spec windowsRestartTargetSpec) []uint32 {
	remaining := make([]uint32, 0, len(processIDs))
	for _, processID := range processIDs {
		running, err := processIDRunning(int(processID))
		if (err != nil || running) && windowsProcessMatchesRestartTarget(processID, spec) {
			remaining = append(remaining, processID)
		}
	}
	return remaining
}

func applyProxyEnvironment(env []string) []savedEnvironmentValue {
	previous := make([]savedEnvironmentValue, 0, len(env))
	seen := map[string]struct{}{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		canonicalKey := strings.ToUpper(key)
		if !ok || key == "" {
			continue
		}
		if _, exists := seen[canonicalKey]; exists {
			continue
		}
		seen[canonicalKey] = struct{}{}
		current, exists := os.LookupEnv(key)
		previous = append(previous, savedEnvironmentValue{key: key, value: current, ok: exists})
		_ = os.Setenv(key, value)
	}
	return previous
}

func restoreProxyEnvironment(previous []savedEnvironmentValue) {
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
