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
	mockCfg, errMock := config.LoadMockProvider(config.OSLookup)
	if err := errors.Join(errCommon, errHTTP, errMock); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	logger := logging.New("mockprovider", common.LogLevel)
	logger.Info("faults", "latency", mockCfg.LatencyMin.String()+"-"+mockCfg.LatencyMax.String(),
		"error_rate", mockCfg.ErrorRate, "throttle_rate", mockCfg.ThrottleRate, "slow_rate", mockCfg.SlowRate,
		"slow_delay", mockCfg.SlowDelay.String(), "reject_rate", mockCfg.RejectRate, "rate_limit", mockCfg.RateLimit)

	srv := &http.Server{
		Addr:              httpCfg.Addr,
		Handler:           mockprovider.New(mockCfg, uint64(time.Now().UnixNano())).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return httpx.ListenAndServe(ctx, srv, httpCfg.ShutdownTimeout, logger)
}
