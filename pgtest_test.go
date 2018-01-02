package pgtest

import (
	"database/sql"
	"testing"
)

func TestWithDatabase_Docker(t *testing.T) {
	InstanceProvider = &pgDockerProvider{}

	var result bool
	WithDatabase(t, NoSetup, func(db *sql.DB) {
		row := db.QueryRow("SELECT TRUE")
		row.Scan(&result)
	})

	if !result {
		t.Fatal("sql statement was not executed successful")
	}
}

