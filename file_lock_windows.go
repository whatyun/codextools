//go:build windows

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

type platformFileLock struct {
	file       *os.File
	lockPath   string
	overlapped windows.Overlapped
}

func tryAcquireFileLock(path string) (fileLockGuard, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	lock := &platformFileLock{file: file, lockPath: path}
	err = windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&lock.overlapped,
	)
	if err != nil {
		_ = file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, &lockBusyError{path: path}
		}
		return nil, err
	}
	return lock, nil
}

func (lock *platformFileLock) release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	unlockErr := windows.UnlockFileEx(windows.Handle(lock.file.Fd()), 0, 1, 0, &lock.overlapped)
	closeErr := lock.file.Close()
	lock.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

func (lock *platformFileLock) path() string {
	if lock == nil {
		return ""
	}
	return lock.lockPath
}
