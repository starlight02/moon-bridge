package app

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"moonbridge/internal/anthropic"
	"moonbridge/internal/bridge"
	"moonbridge/internal/cache"
	"moonbridge/internal/config"
	"moonbridge/internal/server"
)

const Name = "Moon Bridge"

func Run(output io.Writer) {
	fmt.Fprintln(output, WelcomeMessage())
}

func WelcomeMessage() string {
	return "Welcome to " + Name + "!"
}

func RunServerFromEnv(ctx context.Context, errors io.Writer) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	return RunServer(ctx, cfg, errors)
}

func RunServer(ctx context.Context, cfg config.Config, errors io.Writer) error {
	anthropicClient := anthropic.NewClient(anthropic.ClientConfig{
		BaseURL: cfg.ProviderBaseURL,
		APIKey:  cfg.ProviderAPIKey,
		Version: cfg.ProviderVersion,
	})
	handler := server.New(server.Config{
		Bridge:   bridge.New(cfg, cache.NewMemoryRegistry()),
		Provider: anthropicClientWrapper{client: anthropicClient},
	})

	httpServer := &http.Server{Addr: cfg.Addr, Handler: handler}
	errCh := make(chan error, 1)
	go func() {
		fmt.Fprintf(errors, "%s listening on %s\n", Name, cfg.Addr)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

type anthropicClientWrapper struct {
	client *anthropic.Client
}

func (wrapper anthropicClientWrapper) CreateMessage(ctx context.Context, request anthropic.MessageRequest) (anthropic.MessageResponse, error) {
	return wrapper.client.CreateMessage(ctx, request)
}

func (wrapper anthropicClientWrapper) StreamMessage(ctx context.Context, request anthropic.MessageRequest) (anthropic.Stream, error) {
	return wrapper.client.StreamMessage(ctx, request)
}
