//go:build linux
// +build linux

package pgtest

import (
	"os/exec"
	"syscall"
)

func modifyProcessOnSystem(cmd *exec.Cmd) {
	// bind the lifetime of the child process to this process.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
		Setpgid:   true,
	}
}
