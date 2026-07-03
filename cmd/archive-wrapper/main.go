package main

import (
	"context"
	"log"
	"os"

	"github.com/node101-io/archive-wrapper/config"
)

func main() {

	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	if err := run(os.Args[1:], ctx, cfg); err != nil {
		log.Fatal(err)
	}
}
