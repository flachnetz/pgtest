// +build darwin

package pgtest

import (
	"os/exec"
	"syscall"
)

func modifyProcessOnSystem(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}
