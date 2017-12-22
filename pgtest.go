package pgtest

import (
	"errors"
	"fmt"
	"github.com/jmoiron/sqlx"
	"os/exec"
	"testing"
	"time"

	"bufio"
	_ "github.com/lib/pq"
	"io"
	"math/rand"
	"runtime"
)

type SetupFunc func(db *sqlx.DB) error

type TestFunc func(db *sqlx.DB)

func WithDatabase(t *testing.T, setup SetupFunc, fn TestFunc) {
	pg, err := NewPostgresCommand(t)
	if err != nil {
		t.Fatal("Error starting local postgres instance: ", err)
		return
	}

	defer pg.Close()

	db := pg.Connect()
	defer db.Close()

	if err := setup(db); err != nil {
		t.Fatal("Setup database failed: ", err)
		return
	}

	fn(db)
}

func NoSetup(*sqlx.DB) error {
	return nil
}

type postgresCommand struct {
	t           *testing.T
	port        int
	closeSignal chan bool
}

// Dockerfile for an optimized postgres container:
// FROM postgres:10.1-alpine
// ENV PGDATA=/pg
// RUN /docker-entrypoint.sh postgres --version
// USER postgres
// ENTRYPOINT ["/usr/local/bin/postgres", "-D", "/pg"]

func NewPostgresCommand(t *testing.T) (*postgresCommand, error) {
	port := rand.Intn(10000) + 20000

	closeSignal := make(chan bool)

	cmd := exec.Command("docker", "run",
		"--rm", "--name", fmt.Sprintf("postgres-test-%d", port), "-p", fmt.Sprintf("%d:5432", port),
		"flachnetz/pgtest:10.1-1")

	cmd.Stderr = loggerToWriter("[postgres-out]", t.Log)
	cmd.Stdout = loggerToWriter("[postgres-err]", t.Log)

	t.Logf("[postgres] Starting new postgres instance on random port %d", port)
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	go func() {
		cmd.Wait()
		close(closeSignal)
	}()

	return &postgresCommand{
		t:           t,
		port:        port,
		closeSignal: closeSignal,
	}, nil
}

func (cmd *postgresCommand) Close() {
	cmd.t.Log("[postgres] Stopping instance now.")

	// run a command to stop and remove the postgres container.
	exec.Command("docker", "rm", "-fv", fmt.Sprintf("postgres-test-%d", cmd.port)).Run()

	// wait for postgres to stop.
	<-cmd.closeSignal
}

func (cmd *postgresCommand) Connect() *sqlx.DB {
	for idx := 0; idx < 100; idx++ {
		time.Sleep(250 * time.Millisecond)

		cmd.t.Log("Trying to connect to postgres database")
		db, err := sqlx.Connect("postgres",
			fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/postgres?sslmode=disable", cmd.port))

		if err != nil {
			cmd.t.Log("Could not connect to database, trying again: ", err)
			continue
		}

		return db
	}

	panic(errors.New("could not connect to postgres"))
}

func loggerToWriter(prefix string, printFunc func(args ...interface{})) io.Writer {
	reader, writer := io.Pipe()

	go writerScanner(reader, prefix, printFunc)
	runtime.SetFinalizer(writer, writerFinalizer)

	return writer
}

func writerScanner(reader *io.PipeReader, prefix string, printFunc func(args ...interface{})) {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		if scanner.Text() != "" {
			printFunc(prefix, " ", scanner.Text())
		}
	}

	if err := scanner.Err(); err != nil {
		printFunc("Error while reading from Writer: %s", err)
	}

	reader.Close()
}

func writerFinalizer(writer *io.PipeWriter) {
	writer.Close()
}
