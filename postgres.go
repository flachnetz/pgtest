package pgtest

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gofrs/flock"
	"github.com/pkg/errors"
)

type Process struct {
	port     int
	data     string
	lock     *flock.Flock
	cmd      *exec.Cmd
	children sync.WaitGroup
}

type Config struct {
	Binary   string
	Snapshot string
	Workdir  string
}

func Start(config Config) (*Process, error) {
	tempdir := os.TempDir()

	pgdata, err := prepareSnapshot(config)
	if err != nil {
		return nil, errors.WithMessage(err, "prepare snapshot")
	}

	port, lock, err := lockInstancePort(tempdir)
	if err != nil {
		return nil, errors.WithMessage(err, "get instance port")
	}

	instance := &Process{
		data: pgdata,
		port: port,
		lock: lock,
		cmd: exec.Command(config.Binary,
			"-F",
			"-D", pgdata+"/pgdata",
			"-p", strconv.Itoa(port),
			"-c", "listen_addresses=",
			"-c", "autovacuum=off",
			"-c", "unix_socket_directories="+pgdata),
	}

	fmt.Println(instance.cmd.Args)

	instance.cmd.Stderr = logWriter("postgres")
	modifyProcessOnSystem(instance.cmd)

	debugf("Starting new postgres instance on port %d", port)
	if err := instance.cmd.Start(); err != nil {
		_ = instance.Close()
		return nil, errors.WithMessage(err, "starting postgres process")
	}

	return instance, nil
}

func prepareSnapshot(config Config) (string, error) {
	lockfile := filepath.Join(config.Workdir, "snapshots.lock")
	lock := flock.New(lockfile)

	if err := lock.Lock(); err != nil {
		return "", errors.WithMessage(err, "getting lockfile")
	}

	defer lock.Unlock()

	// check for files
	f := os.DirFS(config.Workdir)

	entries, err := fs.ReadDir(f, ".")
	if err != nil {
		return "", errors.WithMessage(err, "read directory")
	}

	var maxIndex int

	pName := regexp.MustCompile("pgtest-([0-9]+)")

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		match := pName.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}

		idx, _ := strconv.Atoi(match[1])
		maxIndex = max(maxIndex, idx)

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if time.Since(info.ModTime()) > 10*time.Minute {
			// found an old snapshot to cleanup
			_ = os.RemoveAll(filepath.Join(config.Workdir, entry.Name()))
		}
	}

	pgdata := filepath.Join(config.Workdir, fmt.Sprintf("pgtest-%d", maxIndex+1))

	debugf("Setup pgdata at %s", pgdata)
	if err := exec.Command("cp", "-r", config.Snapshot, pgdata).Run(); err != nil {
		return "", errors.WithMessagef(err, "copy snapshot to %q", pgdata)
	}

	return pgdata, nil
}

func (proc *Process) Close() error {
	debugf("Waiting for all children of postgres to close (port %d)", proc.port)
	proc.children.Wait()

	debugf("Stopping postgres instance on port %d", proc.port)

	if proc.cmd.Process != nil {
		pgid, err := syscall.Getpgid(proc.cmd.Process.Pid)
		if err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}

		_ = proc.cmd.Wait()
	}

	// the process should now be stopped. we can free the lock
	// and let another instance run on this port.
	if err := proc.lock.Unlock(); err != nil {
		log("could not release postgres instance lock: ", err)
	}

	err := os.RemoveAll(proc.data)
	return errors.WithMessage(err, "cleanup of pgdata")
}

func (proc *Process) dns(dbname string) string {
	return fmt.Sprintf(
		"user=postgres host='%s' port=%d dbname='%s' sslmode=disable",
		proc.data, proc.port, dbname,
	)
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

var instance atomic.Int32

type Instance struct {
	URL    string
	dbname string
	proc   *Process
}

func (proc *Process) Child(ctx context.Context) (*Instance, error) {
	pool, err := connect(ctx, proc.dns("postgres"))
	if err != nil {
		return nil, errors.WithMessage(err, "connect to master instance")
	}

	defer pool.Close()

	dbname := fmt.Sprintf("db%d", instance.Add(1))
	if _, err := pool.ExecContext(ctx, "CREATE DATABASE "+dbname); err != nil {
		return nil, errors.WithMessage(err, "create child database")
	}

	inst := Instance{
		URL:    proc.dns(dbname),
		proc:   proc,
		dbname: dbname,
	}

	// register us as a new child
	proc.children.Add(1)

	return &inst, nil
}

func (inst *Instance) Close() error {
	cleanup := func() {
		db, err := connect(context.Background(), inst.URL)
		if err != nil {
			return
		}

		defer db.Close()

		// cleanup in background to save some space
		_, _ = db.Exec("DROP DATABASE " + inst.dbname)
	}

	go cleanup()

	// tell the parent that we're done here
	inst.proc.children.Done()
	return nil
}
