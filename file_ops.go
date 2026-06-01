package main

import (
	"io"
	"os"
	"path/filepath"
)

func copyDirectory(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFileIfExists(source, target)
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		dest := filepath.Join(target, relative)
		if entry.IsDir() {
			entryInfo, err := entry.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(dest, entryInfo.Mode().Perm())
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		return copyFile(path, dest, entryInfo.Mode().Perm())
	})
}

func copyFile(source, target string, perm os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func replaceDirectory(target, source string) error {
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if !isDir(source) {
		return nil
	}
	return copyDirectory(source, target)
}

func directorySize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
