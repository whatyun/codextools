package main

import (
	"path"
	"strconv"
	"strings"
)

type windowsTCPPortProcessIDs struct {
	Listening []uint32
	Bound     []uint32
}

func parseWindowsTCPListenerProcessIDs(output string, port uint16) []uint32 {
	return parseWindowsTCPPortProcessIDs(output, port).Listening
}

func parseWindowsTCPPortProcessIDs(output string, port uint16) windowsTCPPortProcessIDs {
	if port == 0 {
		return windowsTCPPortProcessIDs{}
	}
	seenListening := map[uint32]bool{}
	seenBound := map[uint32]bool{}
	result := windowsTCPPortProcessIDs{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.EqualFold(fields[0], "TCP") {
			continue
		}
		state := strings.ToUpper(fields[len(fields)-2])
		if state != "LISTENING" && state != "BOUND" {
			continue
		}
		separator := strings.LastIndex(fields[1], ":")
		if separator < 0 {
			continue
		}
		localHost := strings.TrimSpace(fields[1][:separator])
		if localHost != "127.0.0.1" && localHost != "0.0.0.0" {
			continue
		}
		localPort, err := strconv.ParseUint(fields[1][separator+1:], 10, 16)
		if err != nil || uint16(localPort) != port {
			continue
		}
		owner, err := strconv.ParseUint(fields[len(fields)-1], 10, 32)
		processID := uint32(owner)
		if err != nil || processID == 0 {
			continue
		}
		if state == "LISTENING" && !seenListening[processID] {
			seenListening[processID] = true
			result.Listening = append(result.Listening, processID)
		}
		if state == "BOUND" && !seenBound[processID] {
			seenBound[processID] = true
			result.Bound = append(result.Bound, processID)
		}
	}
	return result
}

func windowsRestartTargetProcessMatches(name, packageFamily, imagePath, expectedPackageFamily, expectedExecutablePath string) bool {
	if !isWindowsTargetAppExecutableName(name) {
		return false
	}
	if expectedPackageFamily != "" && strings.EqualFold(strings.TrimSpace(packageFamily), strings.TrimSpace(expectedPackageFamily)) {
		return true
	}
	return windowsExecutablePathsEqual(imagePath, expectedExecutablePath)
}

func windowsExecutablePathsEqual(left, right string) bool {
	left = normalizeWindowsExecutablePath(left)
	right = normalizeWindowsExecutablePath(right)
	return left != "" && right != "" && left == right
}

func normalizeWindowsExecutablePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	if strings.HasPrefix(strings.ToLower(value), "//?/unc/") {
		value = "//" + value[len("//?/unc/"):]
	} else if strings.HasPrefix(strings.ToLower(value), "//?/") {
		value = value[len("//?/"):]
	}
	if value == "" {
		return ""
	}
	return strings.ToLower(path.Clean(value))
}
