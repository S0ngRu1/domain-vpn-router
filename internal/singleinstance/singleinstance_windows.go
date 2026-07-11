//go:build windows

package singleinstance

import (
	"fmt"
	"syscall"
	"unsafe"
)

const errorAlreadyExists syscall.Errno = 183

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutex = kernel32.NewProc("CreateMutexW")
	procCloseHandle = kernel32.NewProc("CloseHandle")
)

type Lock struct {
	handle uintptr
}

func Acquire(name string) (*Lock, bool, error) {
	ptr, err := syscall.UTF16PtrFromString(`Local\` + name)
	if err != nil {
		return nil, false, err
	}
	handle, _, callErr := procCreateMutex.Call(0, 1, uintptr(unsafe.Pointer(ptr)))
	if handle == 0 {
		return nil, false, fmt.Errorf("创建单实例锁失败: %w", callErr)
	}
	lock := &Lock{handle: handle}
	if callErr == errorAlreadyExists {
		return lock, true, nil
	}
	return lock, false, nil
}

func (l *Lock) Release() {
	if l == nil || l.handle == 0 {
		return
	}
	procCloseHandle.Call(l.handle)
	l.handle = 0
}
