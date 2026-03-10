package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bengobox/ticketing-service/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	application, err := app.New(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize application: %v\n", err)
		os.Exit(1)
	}
	defer application.Close()

	if err := application.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "application error: %v\n", err)
		os.Exit(1)
	}
}
