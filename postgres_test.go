package pgtest

import (
	"database/sql"
	"testing"
)

func Test_WithDatabase(t *testing.T) {
	WithDatabase(t, NoSetup, func(db *sql.DB) {
		_, err := db.Exec("CREATE TABLE myTable (id INTEGER)")
		if err != nil {
			t.Fatal("Could not execute sql statement: ", err)
		}
	})
}
