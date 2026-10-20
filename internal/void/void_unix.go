//go:build !windows && !wasm

package void

import (
	"os"
	"syscall"
)

// On POSIX systems, the Void is provided by the kernel at no extra cost.
// It is the most battle-tested storage device in computing history: in
// production since 1971, zero reported data corruption incidents.
const (
	voidPath    = "/dev/null"
	backendName = "Abyss kernel-native (POSIX)"
)

func openPlatformVoid() (Store, error) {
	f, err := os.OpenFile(voidPath, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return &kernelDevice{File: f, fd: int(f.Fd())}, nil
}

// kernelDevice writes straight to the file descriptor. os.File serializes
// writes on a descriptor; the Void has no write ordering to protect, so
// concurrent sessions go to the kernel in parallel.
type kernelDevice struct {
	*os.File
	fd int
}

func (k *kernelDevice) Write(p []byte) (int, error) {
	for {
		n, err := syscall.Write(k.fd, p)
		if err == syscall.EINTR {
			continue
		}
		if n < 0 {
			n = 0
		}
		return n, err
	}
}
