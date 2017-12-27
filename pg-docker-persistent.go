package pgtest

import (
	"database/sql"
	"fmt"
	"github.com/pkg/errors"
	"math/rand"
	"os/exec"
	"sync"
	"syscall"
	"testing"
)

type pgPersistentDockerProvider struct {
	prep sync.Once
	port int
	name string

	t *testing.T
}

func (p *pgPersistentDockerProvider) log(args ...interface{}) {
	t := p.t

	if t != nil {
		t.Log(args...)
	}
}

func (p *pgPersistentDockerProvider) Start(t *testing.T) (Instance, error) {
	p.t = t

	var err error
	p.prep.Do(func() {
		p.port = random.Intn(10000) + 20000

		name := fmt.Sprintf("postgres-test-%d", p.port)

		// create a new instance on some random port
		cmd := exec.Command("docker", "run", "-i",
			"--rm", "--name", name, "-p", fmt.Sprintf("%d:5432", p.port),
			"flachnetz/pgtest:10.1-1")

		cmd.Stderr = loggerToWriter("[postgres-out]", p.log)
		cmd.Stdout = loggerToWriter("[postgres-err]", p.log)

		// kill the child process if the parent (this process) dies.
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Pdeathsig: syscall.SIGTERM,
		}

		p.log(fmt.Sprintf("[postgres] Starting new postgres instance on random port %d", p.port))
		err = errors.WithMessage(cmd.Start(), "starting postgres container")
	})

	if err != nil {
		return nil, err
	}

	instance := &persistentDockerInstance{
		baseInstance: baseInstance{
			t:   t,
			uri: fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/postgres?sslmode=disable", p.port),
		},
	}

	return instance, nil
}

type persistentDockerInstance struct {
	baseInstance
}

func (cmd *persistentDockerInstance) MustConnect() *sql.DB {
	db := cmd.baseInstance.MustConnect()

	schema := fmt.Sprintf("s%d", rand.Int63())

	if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
		db.Close()
		panic(errors.WithMessage(err, "create schema"))
	}

	if _, err := db.Exec("SET search_path TO " + schema); err != nil {
		db.Close()
		panic(errors.WithMessage(err, "set search path to schema"))
	}

	return db
}

func (cmd *persistentDockerInstance) Close() error {
	return nil
}
