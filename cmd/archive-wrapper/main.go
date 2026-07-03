package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {

	// allows to close the wrapper via CTRL + C
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Allows graceful shutdown with cli command
	ctx, cancel := context.WithCancel(ctx)

	if err := run(os.Args[1:], ctx, cancel); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}

}
