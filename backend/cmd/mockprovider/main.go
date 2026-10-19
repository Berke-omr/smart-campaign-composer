// Command mockprovider runs the fake SMS/email provider the worker sends to.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/config"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/httpx"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/mockprovider"
)

func main() {
	if err := run(); err != nil {
		slog.Error("mockprovider exited with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	common, errCommon := config.LoadCommon(config.OSLookup)
	httpCfg, errHTTP := config.LoadHTTP(config.OSLookup)
	if err := errors.Join(errCommon, errHTTP); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	logger := logging.New("mockprovider", common.LogLevel)

	srv := &http.Server{
		Addr:              httpCfg.Addr,
		Handler:           mockprovider.NewHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return httpx.ListenAndServe(ctx, srv, httpCfg.ShutdownTimeout, logger)
}
