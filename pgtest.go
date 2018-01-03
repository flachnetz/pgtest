package pgtest

import (
	"database/sql"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var Root = os.ExpandEnv("${HOME}/.pgtest")

var random = rand.New(rand.NewSource(time.Now().UnixNano()))

type SetupFunc func(db *sql.DB) error

type TestFunc func(db *sql.DB)

func WithDatabase(t *testing.T, setup SetupFunc, test TestFunc) {
	withCurrentT(t, func() {
		if err := preparePostgresInstallation(Root, true); err != nil {
			t.Fatalf("Could not prepare postgres installation: %s", err)
			return
		}

		config := postgresConfig{
			Binary:   filepath.Join(Root, "unpacked/pgsql/bin/postgres"),
			Snapshot: filepath.Join(Root, "initdb/pgdata"),
			Port:     random.Intn(20000) + 10000,
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
