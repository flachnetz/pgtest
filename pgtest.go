package pgtest

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

var Root = os.ExpandEnv("${HOME}/.pgtest")
var Version = "14.3.0"

var isLinuxSystem = runtime.GOOS == "linux"

type SetupFunc func(db Postgres) error

type TestFunc func(db Postgres)

type Postgres struct {
	*sql.DB
	URL string
}

func WithDatabase(t *testing.T, setup SetupFunc, test TestFunc) {

	withCurrentT(t, func() {
		if err := PreparePostgresInstallation(Root, Version, isLinuxSystem); err != nil {
			t.Fatalf("Could not prepare postgres installation: %s", err)
			return
		}

		config := postgresConfig{
			Binary:   filepath.Join(Root, Version, "unpacked/bin/postgres"),
			Snapshot: filepath.Join(Root, Version, "initdb/pgdata"),
		}

		pg, err := startPostgresInstance(config)
		if err != nil {
			t.Fatalf("Could not start postgres instance: %s", err)
			return
		}

		defer pg.Close()

		db, err := connect(pg.URL)
		if err != nil {
			t.Fatalf("Could not open a database connection to postgres at %s: %s", pg.URL, err)
			return
		}

		defer db.Close()

		info := Postgres{
			DB:  db,
			URL: pg.URL,
		}

		if err := setup(info); err != nil {
			t.Fatalf("Database setup failed: %s", err)
			return
		}

		test(info)
	})
}

func NoSetup(Postgres) error {
	return nil
}

var _ SetupFunc = NoSetup
