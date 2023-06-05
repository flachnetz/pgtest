package pgtest

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/theckman/go-flock"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

type Instance struct {
	Data string
	Port int
	URL  string

	lock *flock.Flock
	cmd  *exec.Cmd
}

type postgresConfig struct {
	Binary   string
	Snapshot string
}

func StartInstance(config postgresConfig) (*Instance, error) {
	tempdir := os.TempDir()

	pgdata, err := os.MkdirTemp(tempdir, "pgdata")
	if err != nil {
		return nil, errors.WithMessage(err, "creating pgdata directory")
	}

	debugf("Setup pgdata at %s", pgdata)
	if err := exec.Command("cp", "-r", config.Snapshot, pgdata).Run(); err != nil {
		return nil, errors.WithMessage(err, "copy snapshot to tempdir")
	}

	port, lock, err := lockInstancePort(tempdir)
	if err != nil {
		return nil, errors.WithMessage(err, "get instance port")
	}

	instance := &Instance{
		Data: pgdata,
		Port: port,
		URL:  fmt.Sprintf("user=postgres host='%s' port=%d sslmode=disable", pgdata, port),
		lock: lock,
		cmd: exec.Command(config.Binary,
			"-F",
			"-D", pgdata+"/pgdata",
			"-p", strconv.Itoa(port),
			"-c", "listen_addresses=",
			"-c", "autovacuum=off",
			"-c", "unix_socket_directories="+pgdata),
	}

	instance.cmd.Stderr = logWriter("postgres")
	modifyProcessOnSystem(instance.cmd)

	debugf("Starting new postgres instance on port %d", port)
	if err := instance.cmd.Start(); err != nil {
		_ = instance.Close()
		return nil, errors.WithMessage(err, "starting postgres process")
	}

	return instance, nil
}

func (instance *Instance) Close() error {
	debugf("Stopping postgres instance on port %d", instance.Port)

	if instance.cmd.Process != nil {
		pgid, err := syscall.Getpgid(instance.cmd.Process.Pid)
		if err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}

		_ = instance.cmd.Wait()
	}

	// the process should now be stopped. we can free the lock
	// and let another instance run on this port.
	if err := instance.lock.Unlock(); err != nil {
		log("could not release postgres instance lock: ", err)
	}

	err := os.RemoveAll(instance.Data)
	return errors.WithMessage(err, "cleanup of pgdata")
}

func lockInstancePort(tempdir string) (int, *flock.Flock, error) {
	for port := 20000; port < 21000; port++ {
		lock := flock.New(filepath.Join(tempdir, fmt.Sprintf("pgtest-%d.lock", port)))

		locked, err := lock.TryLock()
		if err != nil {
			return 0, nil, errors.WithMessage(err, "getting postgres lock")
		}

		if locked {
			return port, lock, nil
		}
	}

	return 0, nil, errors.New("no free port found for postgres")
}
