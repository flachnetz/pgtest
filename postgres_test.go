package pgtest

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	// only register this in test. let the user bring its own pgx version
	_ "github.com/jackc/pgx/v5/stdlib"
)

func Test_WithDatabase(t *testing.T) {
	testCase := func(t *testing.T) {
		db := Connect(t)

		_, err := db.ExecContext(t.Context(), "CREATE TABLE myTable (id integer)")
		require.NoError(t, err, "Could not execute sql statement")
	}

	for idx := range 10 {
		t.Run(fmt.Sprintf("Iteration-%d", idx), testCase)
	}
}

func Benchmark_PostgresStartup(b *testing.B) {
	Connect(b)

	b.ResetTimer()

	for b.Loop() {
		_ = Connect(b).Close()
	}
}
