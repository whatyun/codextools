//go:build !windows

package main

import (
	"errors"
	"os"
	"syscall"
)

type platformFileLock struct {
	file     *os.File
	lockPath string
}

func tryAcquireFileLock(path string) (fileLockGuard, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, &lockBusyError{path: path}
		}
		return nil, err
	}
	return &platformFileLock{file: file, lockPath: path}, nil
}

func (lock *platformFileLock) release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	_ = syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	err := lock.file.Close()
	lock.file = nil
	return err
}

func (lock *platformFileLock) path() string {
	if lock == nil {
		return ""
	}
	return lock.lockPath
}
