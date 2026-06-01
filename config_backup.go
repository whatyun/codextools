package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func writeCodexConfigWithBackup(configPath, contents, label string) (*string, error) {
	var backupPath *string
	info, statErr := os.Stat(configPath)
	if statErr == nil {
		if info.IsDir() {
			return nil, fmt.Errorf("%s 是目录，不能写入 config.toml", configPath)
		}
		path, err := backupCodexConfig(configPath, label)
		if err != nil {
			return nil, err
		}
		backupPath = &path
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return backupPath, err
	}
	if err := atomicWrite(configPath, []byte(contents)); err != nil {
		return backupPath, err
	}
	return backupPath, nil
}
