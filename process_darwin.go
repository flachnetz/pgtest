// +build darwin

package pgtest

func modifyProcessOnSystem(cmd *exec.Cmd) {
	// bind the lifetime of the child process to this process.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}
