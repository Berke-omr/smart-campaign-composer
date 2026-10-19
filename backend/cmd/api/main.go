// Command api serves the REST/SSE API used by the frontend.
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
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/db"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/httpapi"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/httpx"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/segment"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/users"
)

func main() {
	if err := run(); err != nil {
		slog.Error("api exited with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	// ctx is cancelled on Ctrl+C or `docker compose stop` (SIGTERM).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	common, errCommon := config.LoadCommon(config.OSLookup)
	httpCfg, errHTTP := config.LoadHTTP(config.OSLookup)
	dbCfg, errDB := config.LoadDatabase(config.OSLookup)
	llmCfg, errLLM := config.LoadLLM(config.OSLookup)
	if err := errors.Join(errCommon, errHTTP, errDB, errLLM); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	logger := logging.New("api", common.LogLevel)

	pool, err := db.Connect(ctx, dbCfg, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	userRepo := users.NewRepository(pool)

	// The segment registry's city list comes from the data, so the LLM can
	// only target cities that exist. Read once at startup (re-seeding with
	// different cities needs an api restart).
	cities, err := userRepo.DistinctCities(ctx)
	if err != nil {
		return fmt.Errorf("load cities: %w", err)
	}
	if len(cities) == 0 {
		logger.Warn("no users in the database; the city field is not targetable until the api restarts after seeding")
	}
	logger.Info("segment registry ready", "cities", len(cities))

	llmInfo := httpapi.LLMInfo{Mode: string(llmCfg.Mode)}
	if llmCfg.Mode == config.LLMModeAnthropic {
		llmInfo.Model = &llmCfg.Model
	}
	logger.Info("llm ready", "mode", llmInfo.Mode, "model", llmCfg.Model)

	handler := httpapi.NewRouter(httpapi.Deps{
		Logger:    logger,
		UserStats: userRepo,
		Segments:  segment.NewRegistry(cities),
		Audience:  segment.NewCounter(pool),
		LLM:       llmInfo,
	})
	srv := &http.Server{
		Addr:              httpCfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		// No WriteTimeout on purpose: SSE responses (live drafts, dashboard)
		// stay open for minutes.
	}
	return httpx.ListenAndServe(ctx, srv, httpCfg.ShutdownTimeout, logger)
}
