package log

import (
	"context"
	"log/slog"
)

type slogContextKey int

const slogFieldsKey slogContextKey = iota

func With(ctx context.Context, fields ...any) context.Context {
	value, _ := ctx.Value(slogFieldsKey).([]any)
	return context.WithValue(ctx, slogFieldsKey, append(value, fields...))
}

func getFields(ctx context.Context) []any {
	value, _ := ctx.Value(slogFieldsKey).([]any)
	return value
}

func log(ctx context.Context, level slog.Level, message string, args ...any) {
	fields := getFields(ctx)
	for _, arg := range args {
		if err, ok := arg.(error); ok {
			fields = append(fields, "error", err)
			continue
		}

		fields = append(fields, arg)
	}

	slog.Log(ctx, level, message, fields...)
}

func Debug(ctx context.Context, message string, args ...any) {
	log(ctx, slog.LevelDebug, message, args...)
}

func Info(ctx context.Context, message string, args ...any) {
	log(ctx, slog.LevelInfo, message, args...)
}

func Warn(ctx context.Context, message string, args ...any) {
	log(ctx, slog.LevelWarn, message, args...)
}

func Error(ctx context.Context, message string, args ...any) {
	log(ctx, slog.LevelError, message, args...)
}
