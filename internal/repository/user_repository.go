package repository

import (
	"context"
	"fmt"
	"websocket-challenge/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserRepository interface {
	CreateUser(ctx context.Context, user *models.User) error
	GetUserByUsername(ctx context.Context, username string) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (*models.User, error)
}

type PostgresUserRepo struct {
	pool *pgxpool.Pool
}

func NewPoolConnection(pool *pgxpool.Pool) UserRepository {
	return &PostgresUserRepo{
		pool: pool,
	}
}

func (r *PostgresUserRepo) CreateUser(ctx context.Context, user *models.User) error {
	const query = `INSERT INTO users (id, username, email, password_hash) VALUES ($1,$2,$3,$4) RETURNING username, created_at, updated_at`
	err := r.pool.QueryRow(ctx, query,
		user.ID,
		user.Username,
		user.Email,
		user.Password_Hash,
	).Scan(&user.Username, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to insert user: %w", err)
	}

	return nil
}

func (r *PostgresUserRepo) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	query := `
		SELECT id, username, email, password_hash, created_at, updated_at, is_banned
		FROM users
		WHERE username = $1`

	user := &models.User{}
	err := r.pool.QueryRow(ctx, query, username).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.Password_Hash,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.IsBanned,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to find user by username: %w", err)
	}

	return user, nil
}

func (r *PostgresUserRepo) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	query := `
		SELECT id, username, email, password_hash, created_at, updated_at, is_banned
		FROM users
		WHERE email = $1`

	user := &models.User{}
	err := r.pool.QueryRow(ctx, query, email).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.Password_Hash,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.IsBanned,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to find user by email: %w", err)
	}

	return user, nil
}

func (r *PostgresUserRepo) GetUserByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	query := `
		SELECT id, username, email, password_hash, created_at, updated_at, is_banned
		FROM users
		WHERE id = $1`

	user := &models.User{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.Password_Hash,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.IsBanned,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to find user by ID: %w", err)
	}

	return user, nil
}
