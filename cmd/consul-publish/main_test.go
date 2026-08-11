package main

import (
	"context"
	"log/slog"
	"testing"
)

func TestNewLoggerLevel(t *testing.T) {
	logger, err := newLogger(LogConfig{Level: "warn", Format: "text"})
	if err != nil {
		t.Fatalf("newLogger() error = %v", err)
	}

	if logger.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("logger enables info records at warn level")
	}
	if !logger.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("logger disables warn records at warn level")
	}
}

func TestNewLoggerRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  LogConfig
	}{
		{name: "level", cfg: LogConfig{Level: "verbose", Format: "text"}},
		{name: "format", cfg: LogConfig{Level: "info", Format: "yaml"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newLogger(tt.cfg); err == nil {
				t.Fatal("newLogger() error = nil, want validation error")
			}
		})
	}
}
