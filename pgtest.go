package pgtest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
)

var (
	Root    = os.ExpandEnv("${HOME}/.pgtest")
	Version = "18.3.0"
)

type SetupFunc func(db *sql.DB) error

// ConnectionString creates a new postgres instance & database and returns the connection string
// to this database as soon as it is reachable.
func ConnectionString(t testing.TB) string {
	t.Helper()

	config, err := Install(t)
	if err != nil {
		t.Fatalf("Could not prepare postgres installation: %s", err)
	}

	pg, err := newInstance(t.Context(), t, config)
	if err != nil {
		t.Fatalf("Failed to start postgres: %s", err)
	}

	t.Cleanup(func() { _ = pg.Close() })

	db, err := connect(t.Context(), t, pg.URL)
	if err != nil {
		t.Fatalf("Could not open a database connection to postgres at %q: %s", pg.URL, err)
	}

	_ = db.Close()

	return pg.URL
}

func Connect(t testing.TB) *sql.DB {
	t.Helper()

	dsn := ConnectionString(t)

	db, err := connect(t.Context(), t, dsn)
	if err != nil {
		t.Fatalf("Could not open a database connection to postgres at %q: %s", dsn, err)
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func ConnectWithSetup(t testing.TB, setup SetupFunc) *sql.DB {
	t.Helper()

	dsn := ConnectionString(t)

	db, err := connect(t.Context(), t, dsn)
	if err != nil {
		t.Fatalf("Could not open a database connection to postgres at %q: %s", dsn, err)
	}

	t.Cleanup(func() { _ = db.Close() })

	if err := setup(db); err != nil {
		t.Fatalf("Failed to setup database: %s", err)
	}

	return db
}

var procsMu sync.Mutex
var procs = map[Config]*pgProcess{}

func newInstance(ctx context.Context, log logger, config Config) (*pgInstance, error) {
	procsMu.Lock()
	defer procsMu.Unlock()

	proc, ok := procs[config]
	if !ok {
		var err error

		proc, err = pgStart(log, config)
		if err != nil {
			return nil, fmt.Errorf("start postgres: %w", err)
		}

		procs[config] = proc
	}

	return proc.Instance(ctx)
}
