package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"aichain/internal/config"
	"aichain/internal/node"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	service, err := node.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := service.Close(); err != nil {
			log.Printf("node close error: %v", err)
		}
	}()

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := service.Run(rootCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
