package main

import (
	"context"
	_ "embed"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"syscall"
	"time"
	_ "time/tzdata" // the distroless image has no zoneinfo; TZ still has to resolve.
)

//go:embed SKILL.md
var skillMarkdown string

func main() {
	arguments := os.Args[1:]
	jsonMode := slices.Contains(arguments, "--json") || slices.Contains(arguments, "-json")
	if err := runCommand(arguments); err != nil {
		printError(err, jsonMode)
		os.Exit(1)
	}
}

func errConfigMissing(name string) *apiError {
	return &apiError{"config_missing", "environment variable " + name + " is not set", 0}
}

func requiredEnv(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", errConfigMissing(name)
	}
	return value, nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func runServe() error {
	apiToken, err := requiredEnv("SHTTODO_API_TOKEN")
	if err != nil {
		return err
	}
	botToken, err := requiredEnv("TELEGRAM_BOT_TOKEN")
	if err != nil {
		return err
	}
	chatIDText, err := requiredEnv("TELEGRAM_CHAT_ID")
	if err != nil {
		return err
	}
	chatID, err := strconv.ParseInt(chatIDText, 10, 64)
	if err != nil {
		return errValidationFailed("invalid TELEGRAM_CHAT_ID %q: must be a number", chatIDText)
	}
	intervalMinutes, err := strconv.Atoi(envOrDefault("REMINDER_INTERVAL_MINUTES", "60"))
	if err != nil || intervalMinutes <= 0 || 1440%intervalMinutes != 0 {
		return errValidationFailed("invalid REMINDER_INTERVAL_MINUTES: must divide 1440, for example 30, 60 or 120")
	}
	activeStartHour, activeEndHour, err := parseActiveHours(envOrDefault("REMINDER_ACTIVE_HOURS", "09-21"))
	if err != nil {
		return errValidationFailed("%v", err)
	}
	store, err := openStore(envOrDefault("SHTTODO_DATABASE_PATH", "shttodo.db"))
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	reminders := &reminder{store, newTelegramBot(botToken, chatID), intervalMinutes, activeStartHour, activeEndHour}
	go reminders.runSchedule(ctx)
	go reminders.pollUpdates(ctx)

	listenAddress := envOrDefault("SHTTODO_LISTEN_ADDRESS", ":8080")
	httpServer := &http.Server{Addr: listenAddress, Handler: (&apiServer{store, apiToken}).routes(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdownCtx)
	}()
	log.Printf("shttodo listening on %s, reminders every %d minutes during %02d-%02d %s",
		listenAddress, intervalMinutes, activeStartHour, activeEndHour, time.Local)
	if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
