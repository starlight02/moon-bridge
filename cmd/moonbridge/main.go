package main

import (
	"context"
	"log"
	"os"

	"moonbridge/internal/app"
)

func main() {
	if err := app.RunServerFromEnv(context.Background(), os.Stderr); err != nil {
		log.Fatal(err)
	}
}
