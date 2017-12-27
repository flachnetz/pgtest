package pgtest

import (
	"fmt"
	"github.com/pkg/errors"
	"os/exec"
	"sync"
	"testing"
)

type pgDockerProvider struct {
	prep sync.Once
}

func (pgDockerProvider) Start(t *testing.T) (Instance, error) {
	port := random.Intn(10000) + 20000

	closeSignal := make(chan bool)

	name := fmt.Sprintf("postgres-test-%d", port)
	cmd := exec.Command("docker", "run",
		"--rm", "--name", name, "-p", fmt.Sprintf("%d:5432", port),
		"flachnetz/pgtest:10.1-1")

	cmd.Stderr = loggerToWriter("[postgres-out]", t.Log)
	cmd.Stdout = loggerToWriter("[postgres-err]", t.Log)

	t.Logf("[postgres] Starting new postgres instance on random port %d", port)
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	go func() {
		cmd.Wait()
		close(closeSignal)
	}()

	return &dockerInstance{
		baseInstance: baseInstance{
			t:   t,
			uri: fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/postgres?sslmode=disable", port),
		},
		name:        name,
		closeSignal: closeSignal,
	}, nil
}

type dockerInstance struct {
	baseInstance
	name        string
	closeSignal chan bool
}

func (cmd *dockerInstance) Close() error {
	cmd.t.Log("[postgres] Stopping instance now.")

	// run a command to stop and remove the postgres container.
	err := exec.Command("docker", "rm", "-fv", cmd.name).Run()
	if err != nil {
		return errors.WithMessage(err, "stopping container")
	}

	// wait for postgres to stop.
	<-cmd.closeSignal

	return nil
}
