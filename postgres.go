package pgtest

import (
	"fmt"
	"github.com/pkg/errors"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

type postgresInstance struct {
	Data string
	Port int
	URL  string

	cmd *exec.Cmd
}

type postgresConfig struct {
	Binary   string
	Snapshot string
	Port     int
}

func startPostgresInstance(config postgresConfig) (*postgresInstance, error) {
	pgdata, err := ioutil.TempDir(os.TempDir(), "pgdata")
	if err != nil {
		return nil, errors.WithMessage(err, "creating pgdata directory")
	}

	debugf("Setup pgdata at %s", pgdata)
	if err := exec.Command("cp", "-r", config.Snapshot, pgdata).Run(); err != nil {
		return nil, errors.WithMessage(err, "copy snapshot to tempdir")
	}

	instance := &postgresInstance{
		Data: pgdata,
		Port: config.Port,
		URL:  fmt.Sprintf("user=postgres host='%s' port=%d sslmode=disable", pgdata, config.Port),
		cmd: exec.Command(config.Binary,
			"-F",
			"-D", pgdata+"/pgdata",
			"-p", strconv.Itoa(config.Port),
			"-c", "listen_addresses=",
			"-c", "autovacuum=off",
			"-c", "unix_socket_directories="+pgdata),
	}

	instance.cmd.Stderr = logWriter("postgres")
	modifyProcessOnSystem(instance.cmd)

	debugf("Starting new postgres instance on port %d", config.Port)
	if err := instance.cmd.Start(); err != nil {
		instance.Close()
		return nil, errors.WithMessage(err, "starting postgres process")
	}

	return instance, nil
}

func (instance *postgresInstance) Close() error {
	debugf("Stopping postgres instance on port %d", instance.Port)

	if instance.cmd.Process != nil {
		pgid, err := syscall.Getpgid(instance.cmd.Process.Pid)
		if err == nil {
			syscall.Kill(-pgid, syscall.SIGKILL)
		}

		instance.cmd.Wait()
	}

	err := os.RemoveAll(instance.Data)
	return errors.WithMessage(err, "cleanup of pgdata")
}
