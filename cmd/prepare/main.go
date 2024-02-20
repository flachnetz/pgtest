package main

import (
	"log"
	"runtime"

	"github.com/flachnetz/pgtest"
)

func main() {
	linux := runtime.GOOS == "linux"
	arch := runtime.GOARCH

	err := pgtest.PreparePostgresInstallation(pgtest.Root, pgtest.Version, linux, arch)
	if err != nil {
		log.Fatalf("postgres setup failed: %s", err)
	}
}
