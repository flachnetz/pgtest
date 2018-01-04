package main

import (
	"github.com/flachnetz/pgtest"
	"log"
	"runtime"
)

func main() {
	linux := runtime.GOOS == "linux"
	err := pgtest.PreparePostgresInstallation(pgtest.Root, linux)
	if err != nil {
		log.Fatalf("postgres setup failed: %s", err)
	}
}
