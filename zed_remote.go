package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type zedRemoteError struct {
	message string
}

func (e zedRemoteError) Error() string {
	return e.message
}

type sshTarget struct {
	User string
	Host string
	Port *uint16
}

func zedRemoteStatusValue() map[string]any {
	appPath := findZedAppPath()
	cliPath := executableInPath("zed")
	platformSupported := runtime.GOOS == "darwin" || runtime.GOOS == "windows" || runtime.GOOS == "linux"
	status := "ok"
	if !platformSupported {
		status = "failed"
	}
	return map[string]any{
		"status":            status,
		"platformSupported": platformSupported,
		"zedAppFound":       appPath != "",
		"zedCliFound":       cliPath != "",
		"zedAppPath":        appPath,
		"zedCliPath":        cliPath,
	}
}

func findZedAppPath() string {
	candidates := []string{
		"/Applications/Zed.app",
		"/Applications/Zed Preview.app",
		"/Applications/Zed Nightly.app",
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates,
			filepath.Join(home, "Applications", "Zed.app"),
			filepath.Join(home, "Applications", "Zed Preview.app"),
			filepath.Join(home, "Applications", "Zed Nightly.app"),
		)
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

func splitSSHAuthority(value string) (string, string, *uint16, error) {
	authority := strings.TrimSpace(value)
	if authority == "" {
		return "", "", nil, nil
	}
	user := ""
	if index := strings.LastIndex(authority, "@"); index >= 0 {
		user = strings.TrimSpace(authority[:index])
		authority = authority[index+1:]
	}
	if strings.HasPrefix(authority, "[") {
		if closeIndex := strings.Index(authority, "]"); closeIndex >= 0 {
			host := strings.TrimSpace(authority[:closeIndex+1])
			suffix := authority[closeIndex+1:]
			var port *uint16
			if strings.HasPrefix(suffix, ":") {
				parsed, err := parseSSHPort(strings.TrimPrefix(suffix, ":"))
				if err != nil {
					return "", "", nil, err
				}
				port = parsed
			}
			return user, host, port, nil
		}
		return user, strings.TrimSpace(authority), nil, nil
	}
	if strings.Count(authority, ":") == 1 {
		host, rawPort, _ := strings.Cut(authority, ":")
		if rawPort != "" && allASCII(rawPort, func(ch byte) bool { return ch >= '0' && ch <= '9' }) {
			port, err := parseSSHPort(rawPort)
			if err != nil {
				return "", "", nil, err
			}
			return user, strings.TrimSpace(host), port, nil
		}
	}
	return user, strings.TrimSpace(authority), nil, nil
}

func parseSSHPort(value string) (*uint16, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 16)
	if err != nil || parsed == 0 {
		return nil, zedRemoteError{"Invalid SSH port"}
	}
	port := uint16(parsed)
	return &port, nil
}

func parseSSHPortAny(value any) (*uint16, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case float64:
		if typed <= 0 || typed > 65535 || typed != float64(uint16(typed)) {
			return nil, zedRemoteError{"Invalid SSH port"}
		}
		port := uint16(typed)
		return &port, nil
	case int:
		if typed <= 0 || typed > 65535 {
			return nil, zedRemoteError{"Invalid SSH port"}
		}
		port := uint16(typed)
		return &port, nil
	case string:
		return parseSSHPort(typed)
	default:
		return nil, zedRemoteError{"Invalid SSH port"}
	}
}

func validateSSHHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", zedRemoteError{"Cannot determine remote SSH host for this file"}
	}
	for _, ch := range host {
		if ch <= 0x20 || strings.ContainsRune("/?#@", ch) {
			return "", zedRemoteError{"Invalid SSH host"}
		}
	}
	if strings.HasPrefix(host, "[") || strings.HasSuffix(host, "]") {
		if !(strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]")) {
			return "", zedRemoteError{"Invalid SSH host"}
		}
		if net.ParseIP(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")) == nil {
			return "", zedRemoteError{"Invalid SSH host"}
		}
		return host, nil
	}
	if strings.ContainsAny(host, "[]") {
		return "", zedRemoteError{"Invalid SSH host"}
	}
	return host, nil
}

func sshTargetFromPayload(payload map[string]any) (sshTarget, error) {
	ssh, _ := payload["ssh"].(map[string]any)
	rawHost := firstString(ssh["host"], ssh["hostname"], ssh["hostName"])
	authorityUser, authorityHost, authorityPort, err := splitSSHAuthority(rawHost)
	if err != nil {
		return sshTarget{}, err
	}
	user := firstString(ssh["user"], ssh["username"], authorityUser)
	host, err := validateSSHHost(authorityHost)
	if err != nil {
		return sshTarget{}, err
	}
	port := authorityPort
	if rawPort, ok := ssh["port"]; ok {
		if text, ok := rawPort.(string); !ok || strings.TrimSpace(text) != "" {
			port, err = parseSSHPortAny(rawPort)
			if err != nil {
				return sshTarget{}, err
			}
		}
	}
	return sshTarget{User: user, Host: host, Port: port}, nil
}

func encodeRemotePath(path string) (string, error) {
	if path == "" {
		return "", zedRemoteError{"Remote path is required"}
	}
	if !strings.HasPrefix(path, "/") {
		return "", zedRemoteError{"Remote path must be absolute"}
	}
	segments := strings.Split(path, "/")
	for index, segment := range segments {
		segments[index] = percentEncodeSegment(segment)
	}
	return strings.Join(segments, "/"), nil
}

func buildZedRemoteURL(target sshTarget, path string) (string, error) {
	host, err := validateSSHHost(target.Host)
	if err != nil {
		return "", err
	}
	encodedPath, err := encodeRemotePath(path)
	if err != nil {
		return "", err
	}
	userPrefix := ""
	if strings.TrimSpace(target.User) != "" {
		userPrefix = percentEncodeSegment(strings.TrimSpace(target.User)) + "@"
	}
	portSuffix := ""
	if target.Port != nil {
		portSuffix = ":" + strconv.Itoa(int(*target.Port))
	}
	return "ssh://" + userPrefix + host + portSuffix + encodedPath, nil
}

func percentEncodeSegment(segment string) string {
	var out strings.Builder
	for _, b := range []byte(segment) {
		ch := rune(b)
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '.' || ch == '_' || ch == '~' {
			out.WriteByte(b)
		} else {
			out.WriteString(fmt.Sprintf("%%%02X", b))
		}
	}
	return out.String()
}

func launchZedURL(rawURL string) error {
	if runtime.GOOS == "darwin" {
		if appPath := findZedAppPath(); appPath != "" {
			cmd := exec.Command("open", "-a", appPath, rawURL)
			hideSubprocessWindow(cmd)
			return cmd.Start()
		}
	}
	if cliPath := executableInPath("zed"); cliPath != "" {
		cmd := exec.Command(cliPath, rawURL)
		hideSubprocessWindow(cmd)
		return cmd.Start()
	}
	return zedRemoteError{"Zed is not installed or not available on PATH"}
}

func targetFromManagedRemoteConnection(connection map[string]any) (sshTarget, error) {
	sshHost := firstString(connection["sshHost"], connection["hostname"])
	sshAlias := firstString(connection["sshAlias"], connection["alias"])
	authorityUser, authorityHost, authorityPort, err := splitSSHAuthority(sshHost)
	if err != nil {
		return sshTarget{}, err
	}
	host := firstString(authorityHost, sshAlias)
	user := firstString(connection["sshUser"], connection["user"], authorityUser)
	port := authorityPort
	if rawPort, ok := connection["sshPort"]; ok {
		if text, ok := rawPort.(string); !ok || strings.TrimSpace(text) != "" {
			port, err = parseSSHPortAny(rawPort)
			if err != nil {
				return sshTarget{}, err
			}
		}
	}
	host, err = validateSSHHost(host)
	if err != nil {
		return sshTarget{}, err
	}
	return sshTarget{User: user, Host: host, Port: port}, nil
}

func resolveSSHTargetFromGlobalState(state map[string]any, hostID string) (sshTarget, error) {
	hostID = strings.TrimSpace(hostID)
	if hostID == "" {
		return sshTarget{}, zedRemoteError{"Remote host id is required"}
	}
	connections, _ := state["codex-managed-remote-connections"].([]any)
	for _, item := range connections {
		connection, _ := item.(map[string]any)
		if stringFromAny(connection["hostId"]) != hostID {
			continue
		}
		return targetFromManagedRemoteConnection(connection)
	}
	return sshTarget{}, zedRemoteError{"Cannot resolve remote SSH host for this file"}
}

func resolveSSHTargetForHostID(hostID string) (sshTarget, error) {
	data, err := os.ReadFile(codexGlobalStatePath(codexHomeDir()))
	if err != nil {
		return sshTarget{}, fmt.Errorf("Cannot read Codex remote connection state: %w", err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return sshTarget{}, fmt.Errorf("Cannot parse Codex remote connection state: %w", err)
	}
	return resolveSSHTargetFromGlobalState(state, hostID)
}

func resolveSSHTargetResponse(payload map[string]any) map[string]any {
	target, err := resolveSSHTargetForHostID(stringFromAny(payload["hostId"]))
	if err != nil {
		return map[string]any{"status": "failed", "message": err.Error()}
	}
	return map[string]any{
		"status": "ok",
		"ssh": map[string]any{
			"user": target.User,
			"host": target.Host,
			"port": target.Port,
		},
	}
}

func zedOpenRemote(payload map[string]any) map[string]any {
	target, err := sshTargetFromPayload(payload)
	if err == nil {
		var url string
		url, err = buildZedRemoteURL(target, stringFromAny(payload["path"]))
		if err == nil {
			err = launchZedURL(url)
			if err == nil {
				return map[string]any{"status": "ok", "url": url}
			}
		}
	}
	return map[string]any{"status": "failed", "message": err.Error()}
}

func zedFallbackRequestResponse(payload map[string]any) map[string]any {
	data, err := os.ReadFile(codexGlobalStatePath(codexHomeDir()))
	if err != nil {
		return map[string]any{"status": "failed", "message": "Cannot read Codex remote connection state: " + err.Error()}
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return map[string]any{"status": "failed", "message": "Cannot parse Codex remote connection state: " + err.Error()}
	}
	request, err := fallbackOpenRequestFromGlobalStateWithContext(
		state,
		stringFromAny(payload["hostId"]),
		stringFromAny(payload["threadId"]),
		stringFromAny(payload["workspaceRoot"]),
		stringFromAny(payload["remoteProjectId"]),
	)
	if err != nil {
		return map[string]any{"status": "failed", "message": err.Error()}
	}
	request["status"] = "ok"
	return request
}

func fallbackOpenRequestFromGlobalStateWithContext(state map[string]any, hostID, threadID, workspaceRoot, remoteProjectID string) (map[string]any, error) {
	hint := threadWorkspaceHint(state, threadID)
	selectedHostID := firstString(hostID, hostIDFromHint(hint), state["selected-remote-host-id"])
	hintedPath := firstString(workspaceRoot, workspacePathFromHint(hint), workspaceRootFromSQLite(threadID, ""))
	if strings.HasPrefix(hintedPath, "/") {
		resolvedHostID := hostIDForRemotePath(state, selectedHostID, hintedPath)
		return openRequestForRemotePath(state, resolvedHostID, hintedPath)
	}

	requestedProjectID := strings.TrimSpace(remoteProjectID)
	if requestedProjectID != "" {
		if strings.HasPrefix(requestedProjectID, "/") {
			return openRequestForRemotePath(state, selectedHostID, requestedProjectID)
		}
		for _, project := range orderedRemoteProjectsFromGlobalState(state) {
			if stringFromAny(project["id"]) != requestedProjectID {
				continue
			}
			projectHostID := stringFromAny(project["hostId"])
			if selectedHostID != "" && projectHostID != selectedHostID {
				continue
			}
			return openRequestForRemotePath(state, projectHostID, stringFromAny(project["remotePath"]))
		}
	}

	for _, project := range orderedRemoteProjectsFromGlobalState(state) {
		projectHostID := stringFromAny(project["hostId"])
		remotePath := stringFromAny(project["remotePath"])
		if (selectedHostID == "" || projectHostID == selectedHostID) && strings.HasPrefix(remotePath, "/") {
			return openRequestForRemotePath(state, firstString(selectedHostID, projectHostID), remotePath)
		}
	}
	return nil, zedRemoteError{"Cannot determine remote workspace or file for Zed"}
}

func workspaceRootFromSQLite(threadID, statePath string) string {
	threadID = normalizeThreadID(threadID)
	if threadID == "" {
		return ""
	}
	if statePath == "" {
		statePath = filepath.Join(codexHomeDir(), "state_5.sqlite")
	}
	if !fileExists(statePath) {
		return ""
	}
	db, err := sql.Open("sqlite", statePath)
	if err != nil {
		return ""
	}
	defer db.Close()
	var cwd string
	if err := db.QueryRow("SELECT cwd FROM threads WHERE id = ?1 LIMIT 1", threadID).Scan(&cwd); err != nil {
		return ""
	}
	return strings.TrimSpace(cwd)
}

func orderedRemoteProjectsFromGlobalState(state map[string]any) []map[string]any {
	rawProjects, _ := state["remote-projects"].([]any)
	var projects []map[string]any
	for _, item := range rawProjects {
		project, _ := item.(map[string]any)
		if project != nil {
			projects = append(projects, project)
		}
	}
	rawOrder, _ := state["project-order"].([]any)
	var ordered []map[string]any
	used := map[string]bool{}
	for _, item := range rawOrder {
		id := stringFromAny(item)
		for _, project := range projects {
			if stringFromAny(project["id"]) == id {
				ordered = append(ordered, project)
				used[id] = true
				break
			}
		}
	}
	for _, project := range projects {
		id := stringFromAny(project["id"])
		if !used[id] {
			ordered = append(ordered, project)
		}
	}
	return ordered
}

func workspacePathFromHint(hint any) string {
	switch typed := hint.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		return firstString(typed["remotePath"], typed["remoteWorkspaceRoot"], typed["workspaceRoot"], typed["path"], typed["cwd"])
	default:
		return ""
	}
}

func normalizeThreadID(threadID string) string {
	return strings.TrimPrefix(strings.TrimSpace(threadID), "local:")
}

func hostIDFromHint(hint any) string {
	object, _ := hint.(map[string]any)
	if object == nil {
		return ""
	}
	return firstString(object["hostId"], object["remoteHostId"])
}

func threadWorkspaceHint(state map[string]any, threadID string) any {
	if strings.TrimSpace(threadID) == "" {
		return nil
	}
	hints, _ := state["thread-workspace-root-hints"].(map[string]any)
	if hints == nil {
		return nil
	}
	bare := normalizeThreadID(threadID)
	for _, key := range []string{threadID, bare, "local:" + bare} {
		if value, ok := hints[key]; ok {
			return value
		}
	}
	return nil
}

func hostIDForRemotePath(state map[string]any, preferredHostID, remotePath string) string {
	if strings.TrimSpace(preferredHostID) != "" {
		return strings.TrimSpace(preferredHostID)
	}
	for _, project := range orderedRemoteProjectsFromGlobalState(state) {
		projectPath := strings.TrimRight(stringFromAny(project["remotePath"]), "/")
		if projectPath != "" && (remotePath == projectPath || strings.HasPrefix(remotePath, projectPath+"/")) {
			return stringFromAny(project["hostId"])
		}
	}
	return ""
}

func openRequestForRemotePath(state map[string]any, hostID, remotePath string) (map[string]any, error) {
	if !strings.HasPrefix(remotePath, "/") {
		return nil, zedRemoteError{"Cannot determine remote workspace or file for Zed"}
	}
	if strings.TrimSpace(hostID) == "" {
		return nil, zedRemoteError{"Remote host id is required"}
	}
	target, err := resolveSSHTargetFromGlobalState(state, hostID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"hostId": hostID,
		"ssh": map[string]any{
			"user": target.User,
			"host": target.Host,
			"port": target.Port,
		},
		"path": remotePath,
	}, nil
}

func allASCII(value string, pred func(byte) bool) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if !pred(value[i]) {
			return false
		}
	}
	return true
}
