package pgtest

import (
	"errors"
	"testing"
	"time"

	"bufio"
	"database/sql"
	_ "github.com/lib/pq"
	"io"
	"math/rand"
	"runtime"
)

var random = rand.New(rand.NewSource(time.Now().UnixNano()))

type SetupFunc func(db *sql.DB) error

type TestFunc func(db *sql.DB)

type Instance interface {
	io.Closer
	MustConnect() *sql.DB
}

type Provider interface {
	Start(t *testing.T) (Instance, error)
}

type baseInstance struct {
	uri string
	t   *testing.T
}

func (cmd *baseInstance) MustConnect() *sql.DB {
	for idx := 0; idx < 100; idx++ {
		if idx > 0 {
			time.Sleep(100 * time.Millisecond)
		}

		cmd.t.Log("Trying to connect to postgres database")

		db, err := sql.Open("postgres", cmd.uri)
		if err != nil {
			cmd.t.Log("Could not open connection to database, trying again: ", err)
			continue
		}

		if err := db.Ping(); err != nil {
			db.Close()

			cmd.t.Log("Could not ping database, trying again: ", err)
			continue
		}

		return db
	}

	panic(errors.New("could not connect to postgres"))
}

// use the persistent docker provider for now.
var InstanceProvider Provider = &pgPersistentDockerProvider{}

func WithDatabase(t *testing.T, setup SetupFunc, fn TestFunc) {
	pg, err := InstanceProvider.Start(t)
	if err != nil {
		t.Fatal("Error starting local postgres instance: ", err)
		return
	}

	defer pg.Close()

	db := pg.MustConnect()
	defer db.Close()

	if err := setup(db); err != nil {
		t.Fatal("Setup database failed: ", err)
		return
	}

	fn(db)
}

func NoSetup(*sql.DB) error {
	return nil
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
