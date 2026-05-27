package pgtest

import (
	"context"
	"database/sql"
	"os"
	"runtime"
	"sync"
	"testing"

	"github.com/pkg/errors"
)

var (
	Root    = os.ExpandEnv("${HOME}/.pgtest")
	Version = "17.7.0"
)

var isLinuxSystem = runtime.GOOS == "linux"

type SetupFunc func(db Conn) error

type TestFunc func(db Conn)

type Conn struct {
	*sql.DB
	URL string
}

var (
	procMu sync.Mutex
	procs  map[Config]*Process
)

func WithDatabase(ctx context.Context, t *testing.T, setup SetupFunc, test TestFunc) {
	withCurrentT(t, func() {
		config, err := Install()
		if err != nil {
			t.Fatalf("Could not prepare postgres installation: %s", err)
			return
		}

		pg, err := newInstance(ctx, config)
		if err != nil {
			t.Fatalf("Failed to start postgres: %s", err)
			return
		}

		defer pg.Close()

		db, err := connect(ctx, pg.URL, nil)
		if err != nil {
			t.Fatalf("Could not open a database connection to postgres at %s: %s", pg.URL, err)
			return
		}

		defer db.Close()

		info := Conn{DB: db, URL: pg.URL}

		if err := setup(info); err != nil {
			t.Fatalf("Database setup failed: %s", err)
			return
		}

		test(info)
	})
}

func newInstance(ctx context.Context, config Config) (*Instance, error) {
	procMu.Lock()
	defer procMu.Unlock()

	if procs == nil {
		procs = map[Config]*Process{}
	}

	proc, ok := procs[config]
	if !ok {
		var err error

		proc, err = Start(config)
		if err != nil {
			return nil, errors.WithMessage(err, "start postgres")
		}

		procs[config] = proc
	}

	inst, err := proc.Child(ctx)
	if err != nil {
		delete(procs, config)
		_ = proc.Close()
		return nil, errors.WithMessage(err, "start postgres")
	}

	return inst, nil
}

func NoSetup(Conn) error {
	return nil
}

var _ SetupFunc = NoSetup

// Cleanup stops all cached postgres processes and removes their data directories.
// Call this from TestMain after m.Run() returns:
//
//	func TestMain(m *testing.M) {
//	    code := m.Run()
//	    pgtest.Cleanup()
//	    os.Exit(code)
//	}
func Cleanup() {
	procMu.Lock()
	procsCopy := make([]*Process, 0, len(procs))
	for _, p := range procs {
		procsCopy = append(procsCopy, p)
	}
	procs = nil
	procMu.Unlock()

	for _, p := range procsCopy {
		p.forceClose()
	}
}
