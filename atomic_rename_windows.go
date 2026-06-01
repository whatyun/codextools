//go:build windows

package main

import "golang.org/x/sys/windows"

func replaceFile(source, target string) error {
	return windows.Rename(source, target)
}
