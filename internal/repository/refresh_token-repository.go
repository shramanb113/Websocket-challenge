package repository

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"websocket-challenge/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RefreshTokenRepository interface {
	SaveRefreshToken(ctx context.Context, token *models.RefreshToken) error
	GetTokenByHash(ctx context.Context, hash string) (*models.RefreshToken, error)
	RevokeToken(ctx context.Context, tokenID uuid.UUID) error
	RevokeAllUserTokens(ctx context.Context, userID uuid.UUID) error
	DeleteToken(ctx context.Context, hash string) error
	DeleteExpiredTokens(ctx context.Context) error
}

type PostgresRefreshTokenRepo struct {
	pool *pgxpool.Pool
}

func NewRefreshTokenRepo(pool *pgxpool.Pool) *PostgresRefreshTokenRepo {
	return &PostgresRefreshTokenRepo{
		pool: pool,
	}
}

func (rf *PostgresRefreshTokenRepo) SaveRefreshToken(ctx context.Context, token *models.RefreshToken) error {

	const query = `
        INSERT INTO refresh_tokens (user_id, token_hash, user_agent, client_ip, expires_at) 
        VALUES ($1, $2, $3, $4, $5)`

	_, err := rf.pool.Exec(ctx, query,
		token.UserID,
		token.TokenHashed,
		token.UserAgent,
		token.ClientIP.String(),
		token.ExpiresAt,
	)

	return err
}

func (rf *PostgresRefreshTokenRepo) GetTokenByHash(ctx context.Context, hash string) (*models.RefreshToken, error) {
	var rfToken models.RefreshToken
	var clientIP string

	const query = `
        SELECT id, user_id, token_hash, user_agent, client_ip, expires_at, created_at 
        FROM refresh_tokens
        WHERE token_hash = $1 AND is_revoked = false`

	err := rf.pool.QueryRow(ctx, query, hash).Scan(
		&rfToken.ID,
		&rfToken.UserID,
		&rfToken.TokenHashed,
		&rfToken.UserAgent,
		&clientIP,
		&rfToken.ExpiresAt,
		&rfToken.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("token not found or revoked")
		}
		return nil, err
	}

	rfToken.ClientIP = net.ParseIP(clientIP)

	return &rfToken, nil
}

func (rf *PostgresRefreshTokenRepo) RevokeToken(ctx context.Context, tokenID uuid.UUID) error {

	const query = `
        UPDATE refresh_tokens 
        SET is_revoked = true 
        WHERE id = $1 AND is_revoked = false`

	result, err := rf.pool.Exec(ctx, query, tokenID)
	if err != nil {
		return fmt.Errorf("failed to revoke token: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("token not found or already revoked")
	}

	return nil
}

func (rf *PostgresRefreshTokenRepo) RevokeAllUserTokens(ctx context.Context, userID uuid.UUID) error {
	const query = `
        UPDATE refresh_tokens 
        SET is_revoked = true 
        WHERE user_id = $1 AND is_revoked = false `

	_, err := rf.pool.Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to revoke tokens: %w", err)
	}

	return nil

}

func (rf *PostgresRefreshTokenRepo) DeleteToken(ctx context.Context, hash string) error {
	const query = `DELETE FROM refresh_tokens WHERE token_hash = $1`

	result, err := rf.pool.Exec(ctx, query, hash)
	if err != nil {
		return fmt.Errorf("failed to delete token: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("security alert: refresh token not found for deletion")
	}

	return nil
}

func (rf *PostgresRefreshTokenRepo) DeleteExpiredTokens(ctx context.Context) error {

	const query = `
        DELETE FROM refresh_tokens 
        WHERE expires_at < NOW() 
        OR is_revoked = true`

	result, err := rf.pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to clean up expired tokens: %w", err)
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected > 0 {
		log.Printf("Maintenance: Purged %d expired/revoked sessions", rowsAffected)
	}

	return nil
}
