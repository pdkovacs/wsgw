package conntrack

import (
	"context"
	"fmt"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

const (
	pgConntrackTable = "wsgw_conntrack"
	pgCreateSchemaSQL = `
CREATE TABLE IF NOT EXISTS wsgw_conntrack (
    user_id       TEXT NOT NULL,
    connection_id TEXT NOT NULL,
    date_created  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, connection_id)
);
CREATE INDEX IF NOT EXISTS idx_wsgw_conntrack_user_id ON wsgw_conntrack(user_id);
`
)

type PostgresConntracker struct {
	pool *pgxpool.Pool
}

func (p *PostgresConntracker) AddConnection(ctx context.Context, userId string, connId string) error {
	logger := zerolog.Ctx(ctx).With().Str("unit", "PostgresConntracker").Str("userId", userId).Str("connId", connId).Logger()

	const sql = `INSERT INTO wsgw_conntrack (user_id, connection_id) VALUES ($1, $2) ON CONFLICT (user_id, connection_id) DO NOTHING`
	_, err := p.pool.Exec(ctx, sql, userId, connId)
	if err != nil {
		return fmt.Errorf("failed to add connection %s for user %s: %w", connId, userId, err)
	}

	logger.Debug().Msg("connection added")
	return nil
}

func (p *PostgresConntracker) RemoveConnection(ctx context.Context, userId string, connId string) (bool, error) {
	logger := zerolog.Ctx(ctx).With().Str("unit", "PostgresConntracker").Str("userId", userId).Str("connId", connId).Logger()

	const sql = `DELETE FROM wsgw_conntrack WHERE user_id = $1 AND connection_id = $2`
	tag, err := p.pool.Exec(ctx, sql, userId, connId)
	if err != nil {
		return false, fmt.Errorf("failed to remove connection %s for user %s: %w", connId, userId, err)
	}

	existed := tag.RowsAffected() > 0
	logger.Debug().Bool("existed", existed).Msg("connection removed")
	return existed, nil
}

func (p *PostgresConntracker) GetConnections(ctx context.Context, userId string) ([]string, error) {
	logger := zerolog.Ctx(ctx).With().Str("unit", "PostgresConntracker").Str("userId", userId).Logger()

	const sql = `SELECT connection_id FROM wsgw_conntrack WHERE user_id = $1`
	rows, err := p.pool.Query(ctx, sql, userId)
	if err != nil {
		return nil, fmt.Errorf("failed to query connections for user %s: %w", userId, err)
	}
	defer rows.Close()

	connIds, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("failed to scan connections for user %s: %w", userId, err)
	}

	logger.Debug().Int("count", len(connIds)).Msg("connections retrieved")
	return connIds, nil
}

func (p *PostgresConntracker) Close() {
	p.pool.Close()
}

func NewPostgresConntracker(ctx context.Context, postgresURL string) (*PostgresConntracker, error) {
	cfg, err := pgxpool.ParseConfig(postgresURL)
	if err != nil {
		return nil, fmt.Errorf("invalid postgres URL: %w", err)
	}

	cfg.ConnConfig.Tracer = otelpgx.NewTracer(
		otelpgx.WithIncludeQueryParameters(),
	)

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to create postgres pool: %w", err)
	}

	if err := otelpgx.RecordStats(pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("unable to register postgres pool stats: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("unable to ping postgres at %s:%d: %w", cfg.ConnConfig.Host, cfg.ConnConfig.Port, err)
	}

	if _, err := pool.Exec(ctx, pgCreateSchemaSQL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("unable to ensure conntrack schema: %w", err)
	}

	zerolog.Ctx(ctx).Info().
		Str("host", cfg.ConnConfig.Host).
		Uint16("port", cfg.ConnConfig.Port).
		Str("database", cfg.ConnConfig.Database).
		Str("table", pgConntrackTable).
		Msg("PostgresConntracker initialized")

	return &PostgresConntracker{pool: pool}, nil
}

var _ WsConnections = (*PostgresConntracker)(nil)
