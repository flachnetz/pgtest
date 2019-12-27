package pgtest

import (
	"testing"
)

func Test_WithDatabase(t *testing.T) {
	WithDatabase(t, NoSetup, func(db Postgres) {
		_, err := db.Exec("CREATE TABLE myTable (id INTEGER)")
		if err != nil {
			t.Fatal("Could not execute sql statement: ", err)
		}
	})
}

func Benchmark_PostgresStartup(b *testing.B) {
	WithDatabase(nil, NoSetup, func(db Postgres) {})

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		WithDatabase(nil, NoSetup, func(db Postgres) {})
	}
}
