package main

import (
	"log"
	"runtime"

	"github.com/flachnetz/pgtest"
)

func main() {
	linux := runtime.GOOS == "linux"

	err := pgtest.PreparePostgresInstallation(pgtest.Root, pgtest.Version, linux)
	if err != nil {
		log.Fatalf("postgres setup failed: %s", err)
	}
}
