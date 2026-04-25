package main

import (
	"context"
	"flag"
	"log"
	"os"

	"moonbridge/internal/app"
	"moonbridge/internal/config"
)

func main() {
	configPath := flag.String("config", "", "Path to config.yml")
	addr := flag.String("addr", "", "Override server listen address")
	flag.Parse()

	var cfg config.Config
	var err error
	if *configPath != "" {
		cfg, err = config.LoadFromFile(*configPath)
	} else {
		cfg, err = config.LoadFromEnv()
	}
	if err != nil {
		log.Fatal(err)
	}
	if *addr != "" {
		cfg.Addr = *addr
	}

	if err := app.RunServer(context.Background(), cfg, os.Stderr); err != nil {
		log.Fatal(err)
	}
}
