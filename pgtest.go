package pgtest

import (
	"database/sql"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

var Root = os.ExpandEnv("${HOME}/.pgtest")

var randomLock sync.Mutex
var random = rand.New(rand.NewSource(time.Now().UnixNano()))

var isLinuxSystem = runtime.GOOS == "linux"

type SetupFunc func(db *sql.DB) error

type TestFunc func(db *sql.DB)

func WithDatabase(t *testing.T, setup SetupFunc, test TestFunc) {
	withCurrentT(t, func() {
		if err := PreparePostgresInstallation(Root, isLinuxSystem); err != nil {
			t.Fatalf("Could not prepare postgres installation: %s", err)
			return
		}

		randomLock.Lock()
		port := random.Intn(20000) + 10000
		randomLock.Unlock()

		config := postgresConfig{
			Binary:   filepath.Join(Root, "unpacked/pgsql/bin/postgres"),
			Snapshot: filepath.Join(Root, "initdb/pgdata"),
			Port:     port,
		}

		pg, err := startPostgresInstance(config)
		if err != nil {
			t.Fatalf("Could not start postgres instance on port %d: %s", config.Port, err)
			return
		}

		defer pg.Close()

		db, err := connect(pg.URL)
		if err != nil {
			t.Fatalf("Could not open a database connection to postgres at %s: %s", pg.URL, err)
			return
		}

		defer db.Close()

		if err := setup(db); err != nil {
			t.Fatalf("Database setup failed: %s", err)
			return
		}

		test(db)
	})
}

func NoSetup(*sql.DB) error {
	return nil
}
