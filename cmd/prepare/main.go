package main

import (
	"github.com/flachnetz/pgtest"
	"log"
)

func main() {
	_, err := pgtest.Install()
	if err != nil {
		log.Fatalf("postgres setup failed: %s", err)
	}
}
