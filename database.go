package pgtest

import (
	"context"
	"database/sql"
	"time"
)

func connect(ctx context.Context, uri string) (*sql.DB, error) {
	var err error
	for idx := 0; idx < 100; idx++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if idx > 0 && idx < 20 {
			// poll very quickly in the beginning, postgres is pretty quick to get up
			time.Sleep(25 * time.Millisecond)
		} else if idx >= 40 {
			// ok slow down, maybe there is a problem?
			time.Sleep(100 * time.Millisecond)
		}

		var db *sql.DB
		db, err = sql.Open("pgx", uri)
		if err != nil {
			debugf("sql.Open failed with: %s", err)
			continue
		}

		if err = db.PingContext(ctx); err != nil {
			debugf("sql.Ping failed with: %s", err)

			db.Close()
			continue
		}

		return db, nil
	}

	return nil, err
}
