package main

import (
	"github.com/flachnetz/pgtest"
	"log"
	"runtime"
)

func main() {
	linux := runtime.GOOS == "linux"

	err := pgtest.PreparePostgresInstallation(pgtest.Root, pgtest.Version, linux)
	if err != nil {
		log.Fatalf("postgres setup failed: %s", err)
	}
}
