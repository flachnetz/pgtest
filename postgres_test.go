package pgtest

import (
	"context"
	"testing"

	// only register this in test. let the user bring its own pgx version
	_ "github.com/jackc/pgx/v5/stdlib"
)

var ctx = context.Background()

func Test_WithDatabase(t *testing.T) {
	WithDatabase(ctx, t, NoSetup, func(db Conn) {
		_, err := db.Exec("CREATE TABLE myTable (id integer)")
		if err != nil {
			t.Fatal("Could not execute sql statement: ", err)
		}
	})
}

func Benchmark_PostgresStartup(b *testing.B) {
	WithDatabase(ctx, nil, NoSetup, func(db Conn) {})

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		WithDatabase(ctx, nil, NoSetup, func(db Conn) {})
	}
}
