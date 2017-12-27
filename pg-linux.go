package pgtest

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"testing"
)

type pgLinuxProvider struct {
	prep sync.Once
}

func (pgLinuxProvider) Start(t *testing.T) (Instance, error) {
	port := random.Intn(10000) + 20000

	startScript := os.ExpandEnv("$HOME/.pgtest/pgsql/start.sh")
	cmd := exec.Command(startScript, strconv.Itoa(port))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	cmd.Stderr = loggerToWriter("[postgres-out]", t.Log)
	cmd.Stdout = loggerToWriter("[postgres-err]", t.Log)

	t.Logf("[postgres] Starting new postgres instance on random port %d", port)
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &linuxInstance{
		baseInstance: baseInstance{
			t:   t,
			uri: fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/postgres?sslmode=disable", port),
		},
		cmd: cmd,
	}, nil
}

type linuxInstance struct {
	baseInstance
	cmd *exec.Cmd
}

func (cmd *linuxInstance) Close() error {
	cmd.t.Log("[postgres] Stopping instance now.")

	pgid, err := syscall.Getpgid(cmd.cmd.Process.Pid)
	if err == nil {
		syscall.Kill(-pgid, syscall.SIGINT)
	}

	cmd.cmd.Wait()

	return nil
}
