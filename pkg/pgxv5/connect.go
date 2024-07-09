package pgxv5

import (
	"context"

	"github.com/go-errors/errors"
	"github.com/jackc/pgx/v4"
)

// Extends pgx.Connect to override parsed config programmatically
func ConnectByUrl(ctx context.Context, url string, options ...func(*pgx.ConnConfig)) (*pgx.Conn, error) {
	// Parse connection url
	config, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, errors.Errorf("failed to parse postgres url: %w", err)
	}
	// Apply config overrides
	for _, op := range options {
		op(config)
	}
	// Connect to database
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, errors.Errorf("failed to connect to postgres: %w", err)
	}
	return conn, nil
}
