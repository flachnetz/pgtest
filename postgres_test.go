package pgtest

import (
	"fmt"
	"testing"

	// only register this in test. let the user bring its own pgx version
	_ "github.com/jackc/pgx/v5/stdlib"
)

func Test_WithDatabase(t *testing.T) {
	testCase := func(t *testing.T) {
		db := Connect(t)

		_, err := db.ExecContext(t.Context(), "CREATE TABLE myTable (id integer)")
		if err != nil {
			t.Fatal("Could not execute sql statement: ", err)
		}
	}

	for idx := range 10 {
		t.Run(fmt.Sprintf("Iteration-%d", idx), testCase)
	}
}

func Benchmark_PostgresStartup(b *testing.B) {
	Connect(b)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = Connect(b).Close()
	}
}
