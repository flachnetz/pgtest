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
	Version = "16.1.0"
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

		db, err := connect(ctx, pg.URL)
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

	return proc.Child(ctx)
}

func NoSetup(Conn) error {
	return nil
}

var _ SetupFunc = NoSetup
