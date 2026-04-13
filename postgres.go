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

	"errors"

	"github.com/gofrs/flock"
)

type pgProcess struct {
	log      logger
	port     int
	data     string
	lock     *flock.Flock
	cmd      *exec.Cmd
	children sync.WaitGroup
}

// pgStart will create a new postgres process for the given postgres config.
func pgStart(log logger, config Config) (*pgProcess, error) {
	tempdir := os.TempDir()

	data, err := prepareSnapshot(log, config)
	if err != nil {
		return nil, fmt.Errorf("prepare snapshot: %w", err)
	}

	port, lock, err := lockInstancePort(tempdir)
	if err != nil {
		return nil, fmt.Errorf("get instance port: %w", err)
	}

	instance := &pgProcess{
		log:  log,
		data: data,
		port: port,
		lock: lock,
		cmd: exec.Command(config.Binary,
			"-F",
			"-D", data+"/pgdata",
			"-p", strconv.Itoa(port),
			"-c", "listen_addresses=",
			"-c", "autovacuum=off",
			"-c", "unix_socket_directories="+data),
	}

	fmt.Println(instance.cmd.Args)

	instance.cmd.Stderr = logWriter(log, "postgres")
	modifyProcessOnSystem(instance.cmd)

	log.Logf("Starting new postgres instance on port %d", port)
	if err := instance.cmd.Start(); err != nil {
		_ = instance.Close()
		return nil, fmt.Errorf("starting postgres process")
	}

	return instance, nil
}

func (proc *pgProcess) Close() error {
	proc.log.Logf("Waiting for all children of postgres to close (port %d)", proc.port)
	proc.children.Wait()

	proc.log.Logf("Stopping postgres instance on port %d", proc.port)

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
		proc.log.Logf("could not release postgres instance lock: %s", err)
	}

	if err := proc.lock.Close(); err != nil {
		proc.log.Logf("could not close postgres instance lock: %s", err)
	}

	// remove the process data
	err := os.RemoveAll(proc.data)
	if err != nil {
		return fmt.Errorf("cleanup pgdata: %w", err)
	}

	return nil
}

func (proc *pgProcess) dns(dbname string) string {
	return fmt.Sprintf(
		"user=postgres host='%s' port=%d dbname='%s' sslmode=disable",
		proc.data, proc.port, dbname,
	)
}

func prepareSnapshot(log logger, config Config) (string, error) {
	lockfile := filepath.Join(config.Workdir, "snapshots.lock")

	// open the lock file
	lock := flock.New(lockfile)
	defer func() { _ = lock.Close() }()

	// get an actual lock
	if err := lock.Lock(); err != nil {
		return "", fmt.Errorf("getting lockfile")
	}

	defer func() { _ = lock.Unlock() }()

	// check for files
	f := os.DirFS(config.Workdir)

	entries, err := fs.ReadDir(f, ".")
	if err != nil {
		return "", fmt.Errorf("read directory")
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
			snapshot := filepath.Join(config.Workdir, entry.Name())

			// found an old snapshot to cleanup
			log.Logf("Removing old snapshot at %q", snapshot)
			_ = os.RemoveAll(snapshot)
		}
	}

	data := filepath.Join(config.Workdir, fmt.Sprintf("pgtest-%d", maxIndex+1))

	log.Logf("Setup pgdata at %q", data)
	if err := exec.Command("cp", "-r", config.Snapshot, data).Run(); err != nil {
		return "", fmt.Errorf("copy snapshot to %q: %w", data, err)
	}

	return data, nil
}

func lockInstancePort(tempdir string) (int, *flock.Flock, error) {
	for port := 20000; port < 21000; port++ {
		//goland:noinspection GoResourceLeak
		lock := flock.New(filepath.Join(tempdir, fmt.Sprintf("pgtest-%d.lock", port)))

		locked, err := lock.TryLock()
		if err != nil {
			return 0, nil, fmt.Errorf("getting postgres lock")
		}

		if locked {
			return port, lock, nil
		}

		// close lock file if we did not use it
		_ = lock.Close()
	}

	return 0, nil, errors.New("no free port found for postgres")
}

var instance atomic.Int32

type pgInstance struct {
	URL    string
	dbname string
	proc   *pgProcess
}

// Instance returns a new child instance for this process.
func (proc *pgProcess) Instance(ctx context.Context) (*pgInstance, error) {
	// temporarily register us as a new child
	proc.children.Add(1)
	defer proc.children.Done()

	db, err := connect(ctx, proc.log, proc.dns("postgres"))
	if err != nil {
		return nil, fmt.Errorf("connect to master instance: %w", err)
	}

	defer func() { _ = db.Close() }()

	dbname := fmt.Sprintf("db%d", instance.Add(1))
	if _, err := db.ExecContext(ctx, "CREATE DATABASE "+dbname); err != nil {
		return nil, fmt.Errorf("create child database: %w", err)
	}

	inst := pgInstance{
		URL:    proc.dns(dbname),
		proc:   proc,
		dbname: dbname,
	}

	// success, register us as a new child, will be released in Close()
	proc.children.Add(1)

	return &inst, nil
}

func (inst *pgInstance) Close() error {
	cleanup := func() {
		// wait a moment to give a different test the possibility to grab the
		// parent process before we shut it down.
		time.Sleep(1 * time.Second)

		// tell the process that we're done here
		defer inst.proc.children.Done()

		parentUrl := inst.proc.dns("postgres")
		db, err := connect(context.Background(), inst.proc.log, parentUrl)
		if err != nil {
			return
		}

		defer func() { _ = db.Close() }()

		// cleanup in background to save some space
		_, _ = db.Exec("DROP DATABASE " + inst.dbname)
	}

	// run cleanup in background
	go cleanup()

	return nil
}
