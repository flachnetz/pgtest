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
	log    logger
	port   int
	data   string
	lock   *flock.Flock
	cmd    *exec.Cmd
	config Config

	childMu    sync.Mutex
	childCount int
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
		log:    log,
		data:   data,
		port:   port,
		lock:   lock,
		config: config,
		cmd: exec.Command(config.Binary,
			"-F",
			"-D", data+"/pgdata",
			"-p", strconv.Itoa(port),
			"-c", "listen_addresses=",
			"-c", "autovacuum=off",
			"-c", "unix_socket_directories="+data),
	}

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
	proc.log.Logf("Stopping postgres instance on port %d", proc.port)

	proc.stopProcess()

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

// stopProcess sends SIGINT to the postgres process group for fast shutdown,
// then falls back to SIGKILL if it doesn't exit within 10 seconds. Fast
// shutdown (SIGINT) lets postgres clean up IPC resources (shared memory,
// semaphores). SIGKILL should be avoided as it prevents IPC cleanup.
func (proc *pgProcess) stopProcess() {
	if proc.cmd.Process == nil {
		return
	}

	pgid, err := syscall.Getpgid(proc.cmd.Process.Pid)
	if err != nil {
		_ = proc.cmd.Wait()
		return
	}

	_ = syscall.Kill(-pgid, syscall.SIGINT)

	done := make(chan struct{})
	go func() {
		_ = proc.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = proc.cmd.Wait()
	}
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
	proc.childMu.Lock()
	proc.childCount++
	proc.childMu.Unlock()

	cleanup := true
	defer func() {
		if cleanup {
			proc.childMu.Lock()
			proc.childCount--
			proc.childMu.Unlock()
		}
	}()

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
	proc.childMu.Lock()
	proc.childCount++
	proc.childMu.Unlock()

	return &inst, nil
}

func (inst *pgInstance) Close() error {
	inst.proc.childMu.Lock()
	inst.proc.childCount--
	lastChild := inst.proc.childCount == 0
	inst.proc.childMu.Unlock()

	if lastChild {
		removeFromCache(inst.proc.config)
		inst.proc.stopProcess()
		_ = inst.proc.lock.Unlock()
		_ = inst.proc.lock.Close()
		_ = os.RemoveAll(inst.proc.data)
		return nil
	}

	// Drop the database in the background to avoid blocking the test.
	parentUrl := inst.proc.dns("postgres")
	log := inst.proc.log
	dbname := inst.dbname
	go func() {
		db, err := connect(context.Background(), log, parentUrl)
		if err != nil {
			return
		}

		defer func() { _ = db.Close() }()

		_, _ = db.Exec("DROP DATABASE " + dbname)
	}()

	return nil
}
