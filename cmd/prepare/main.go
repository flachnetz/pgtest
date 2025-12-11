package main

import (
	"log"

	"github.com/flachnetz/pgtest"
)

func main() {
	_, err := pgtest.Install()
	if err != nil {
		log.Fatalf("postgres setup failed: %s", err)
	}
}
