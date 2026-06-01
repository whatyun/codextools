package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const skillMCPBackupKind = "skill-mcp"

func (s *server) listSkillMCPBackups() commandResult {
	backups, err := listSkillMCPBackups(codexHomeDir())
	if err != nil {
		return failed("读取 Skill/MCP 备份失败："+err.Error(), map[string]any{"backups": []skillMCPBackupInfo{}})
	}
	return ok("Skill/MCP 备份已读取。", map[string]any{"backups": backups, "backupRoot": skillMCPBackupRoot(codexHomeDir())})
}

func (s *server) createSkillMCPBackup(args map[string]any) commandResult {
	label := stringArg(mapArg(args, "request"), "label")
	if label == "" {
		label = stringArg(args, "label")
	}
	info, err := createSkillMCPBackup(codexHomeDir(), label)
	if err != nil {
		return failed("创建 Skill/MCP 备份失败："+err.Error(), map[string]any{})
	}
	backups, _ := listSkillMCPBackups(codexHomeDir())
	return ok("Skill/MCP 备份已创建："+info.ID, map[string]any{"backup": info, "backups": backups, "backupRoot": skillMCPBackupRoot(codexHomeDir())})
}

func (s *server) restoreSkillMCPBackup(args map[string]any) commandResult {
	id := firstString(stringArg(mapArg(args, "request"), "id"), stringArg(args, "id"))
	current, restored, err := restoreSkillMCPBackup(codexHomeDir(), id)
	if err != nil {
		return failed("恢复 Skill/MCP 备份失败："+err.Error(), map[string]any{})
	}
	backups, _ := listSkillMCPBackups(codexHomeDir())
	return ok("Skill/MCP 备份已恢复："+restored.ID, map[string]any{
		"backup":        restored,
		"currentBackup": current,
		"backups":       backups,
		"backupRoot":    skillMCPBackupRoot(codexHomeDir()),
	})
}

func (s *server) deleteSkillMCPBackup(args map[string]any) commandResult {
	id := firstString(stringArg(mapArg(args, "request"), "id"), stringArg(args, "id"))
	if err := deleteSkillMCPBackup(codexHomeDir(), id); err != nil {
		return failed("删除 Skill/MCP 备份失败："+err.Error(), map[string]any{})
	}
	backups, _ := listSkillMCPBackups(codexHomeDir())
	return ok("Skill/MCP 备份已删除："+id, map[string]any{"backups": backups, "backupRoot": skillMCPBackupRoot(codexHomeDir())})
}

func skillMCPBackupRoot(home string) string {
	return filepath.Join(home, "backups_state", skillMCPBackupKind)
}

func createSkillMCPBackup(home, label string) (skillMCPBackupInfo, error) {
	if strings.TrimSpace(label) == "" {
		label = "manual"
	}
	root := skillMCPBackupRoot(home)
	id := time.Now().Format("20060102-150405")
	backupDir := filepath.Join(root, id)
	for index := 2; fileExists(backupDir); index++ {
		backupDir = filepath.Join(root, fmt.Sprintf("%s-%d", id, index))
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return skillMCPBackupInfo{}, err
	}
	info := skillMCPBackupInfo{
		ID:        filepath.Base(backupDir),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Label:     label,
		Path:      backupDir,
	}
	dirs := []struct {
		source string
		target string
		flag   *bool
	}{
		{filepath.Join(home, "skills"), filepath.Join(backupDir, "skills"), &info.HasSkills},
		{filepath.Join(home, "plugins", "cache"), filepath.Join(backupDir, "plugins", "cache"), &info.HasPluginCache},
		{filepath.Join(home, ".tmp", "bundled-marketplaces"), filepath.Join(backupDir, ".tmp", "bundled-marketplaces"), &info.HasBundledMarket},
	}
	for _, dir := range dirs {
		if isDir(dir.source) {
			if err := copyDirectory(dir.source, dir.target); err != nil {
				return info, err
			}
			*dir.flag = true
		}
	}
	configSnapshot := extractSkillMCPConfigSnapshot(readFile(filepath.Join(home, "config.toml")))
	if strings.TrimSpace(configSnapshot) != "" {
		path := filepath.Join(backupDir, "config-skill-mcp.toml")
		if err := atomicWrite(path, []byte(configSnapshot)); err != nil {
			return info, err
		}
		info.HasConfigSnapshot = true
		info.ConfigSnapshotBytes = int64(len(configSnapshot))
	}
	info.SizeBytes = directorySize(backupDir)
	if err := writeSkillMCPBackupMetadata(backupDir, info); err != nil {
		return info, err
	}
	return info, nil
}

func listSkillMCPBackups(home string) ([]skillMCPBackupInfo, error) {
	root := skillMCPBackupRoot(home)
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []skillMCPBackupInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	var backups []skillMCPBackupInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := readSkillMCPBackupInfo(filepath.Join(root, entry.Name()))
		if err == nil {
			backups = append(backups, info)
		}
	}
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].ID > backups[j].ID
	})
	return backups, nil
}

func restoreSkillMCPBackup(home, id string) (skillMCPBackupInfo, skillMCPBackupInfo, error) {
	backupDir, err := resolveSkillMCPBackupDir(home, id)
	if err != nil {
		return skillMCPBackupInfo{}, skillMCPBackupInfo{}, err
	}
	if !isDir(backupDir) {
		return skillMCPBackupInfo{}, skillMCPBackupInfo{}, errors.New("备份不存在：" + id)
	}
	current, err := createSkillMCPBackup(home, "pre-restore-"+id)
	if err != nil {
		return current, skillMCPBackupInfo{}, err
	}
	restored, err := readSkillMCPBackupInfo(backupDir)
	if err != nil {
		return current, restored, err
	}
	dirPairs := []struct {
		source string
		target string
	}{
		{filepath.Join(backupDir, "skills"), filepath.Join(home, "skills")},
		{filepath.Join(backupDir, "plugins", "cache"), filepath.Join(home, "plugins", "cache")},
		{filepath.Join(backupDir, ".tmp", "bundled-marketplaces"), filepath.Join(home, ".tmp", "bundled-marketplaces")},
	}
	for _, pair := range dirPairs {
		if isDir(pair.source) {
			if err := replaceDirectory(pair.target, pair.source); err != nil {
				return current, restored, err
			}
		} else if err := os.RemoveAll(pair.target); err != nil {
			return current, restored, err
		}
	}
	configBackup, err := restoreSkillMCPConfigTables(home, filepath.Join(backupDir, "config-skill-mcp.toml"))
	if err != nil {
		return current, restored, err
	}
	if configBackup != nil {
		restored.RestoreConfigBackup = *configBackup
	}
	restored.RestoreSourceBackup = current.ID
	return current, restored, nil
}

func deleteSkillMCPBackup(home, id string) error {
	backupDir, err := resolveSkillMCPBackupDir(home, id)
	if err != nil {
		return err
	}
	if !isDir(backupDir) {
		return errors.New("备份不存在：" + id)
	}
	return os.RemoveAll(backupDir)
}

func resolveSkillMCPBackupDir(home, id string) (string, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return "", errors.New("备份 id 不能为空")
	}
	if filepath.Base(trimmed) != trimmed || strings.Contains(trimmed, "/") || strings.Contains(trimmed, `\`) || strings.Contains(trimmed, "..") {
		return "", errors.New("非法备份 id")
	}
	root, err := filepath.Abs(skillMCPBackupRoot(home))
	if err != nil {
		return "", err
	}
	candidate, err := filepath.Abs(filepath.Join(root, trimmed))
	if err != nil {
		return "", err
	}
	if candidate != root && !strings.HasPrefix(candidate, root+string(os.PathSeparator)) {
		return "", errors.New("非法备份路径")
	}
	return candidate, nil
}

func readSkillMCPBackupInfo(path string) (skillMCPBackupInfo, error) {
	var info skillMCPBackupInfo
	if err := readJSON(filepath.Join(path, "metadata.json"), &info); err != nil {
		info = skillMCPBackupInfo{
			ID:        filepath.Base(path),
			CreatedAt: "",
			Label:     "",
			Path:      path,
		}
	}
	info.ID = filepath.Base(path)
	info.Path = path
	info.HasSkills = isDir(filepath.Join(path, "skills"))
	info.HasPluginCache = isDir(filepath.Join(path, "plugins", "cache"))
	info.HasBundledMarket = isDir(filepath.Join(path, ".tmp", "bundled-marketplaces"))
	configInfo, err := os.Stat(filepath.Join(path, "config-skill-mcp.toml"))
	info.HasConfigSnapshot = err == nil && !configInfo.IsDir()
	if info.HasConfigSnapshot {
		info.ConfigSnapshotBytes = configInfo.Size()
	}
	info.SizeBytes = directorySize(path)
	return info, nil
}

func writeSkillMCPBackupMetadata(path string, info skillMCPBackupInfo) error {
	info.SizeBytes = directorySize(path)
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(path, "metadata.json"), data)
}

func extractSkillMCPConfigSnapshot(contents string) string {
	return extractTomlTables(contents, isSkillMCPConfigTable)
}

func restoreSkillMCPConfigTables(home, snapshotPath string) (*string, error) {
	snapshot, err := os.ReadFile(snapshotPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(home, "config.toml")
	current := readFile(configPath)
	updated := removeTomlTablesMatching(current, isSkillMCPConfigTable)
	if strings.TrimSpace(string(snapshot)) != "" {
		updated = appendTomlBlock(updated, splitLines(string(snapshot)))
	}
	if updated == current {
		return nil, nil
	}
	return writeCodexConfigWithBackup(configPath, updated, "skill-mcp-restore")
}

func isSkillMCPConfigTable(table string) bool {
	table = strings.TrimSpace(table)
	return table == "features" ||
		table == "windows" ||
		strings.HasPrefix(table, "mcp_servers.") ||
		strings.HasPrefix(table, "plugins.") ||
		strings.HasPrefix(table, "marketplaces.")
}
