//go:build !windows

package tui

import (
	"syscall"
	"unsafe"
)

func rawMode() (func(), error) {
	var old syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL,
		uintptr(syscall.Stdin), syscall.TIOCGETA, uintptr(unsafe.Pointer(&old))); e != 0 {
		return func() {}, e
	}
	raw := old
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL,
		uintptr(syscall.Stdin), syscall.TIOCSETA, uintptr(unsafe.Pointer(&raw))); e != 0 {
		return func() {}, e
	}
	restore := func() {
		syscall.Syscall(syscall.SYS_IOCTL,
			uintptr(syscall.Stdin), syscall.TIOCSETA, uintptr(unsafe.Pointer(&old)))
	}
	return restore, nil
}
