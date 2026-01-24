package repository

import (
	"context"
	"time"
	"websocket-challenge/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MessageRepo interface {
	Save(ctx context.Context, message *models.Message) error
	Fetch(ctx context.Context, roomId uuid.UUID, limit int, before time.Time) ([]*models.Message, error)
}

type PostgresMessagesRepo struct {
	pool *pgxpool.Pool
}

func NewMessagesRepo(pool *pgxpool.Pool) MessageRepo {
	return &PostgresMessagesRepo{
		pool: pool,
	}
}

func (r *PostgresMessagesRepo) Save(ctx context.Context, m *models.Message) error {
	query := `
        INSERT INTO messages (id, room_id, sender, content, type, created_at)
        VALUES ($1, $2, $3, $4, $5, $6)
    `

	_, err := r.pool.Exec(ctx, query, m.ID, m.RoomID, m.Sender, m.Content, m.Type, m.CreatedAt)
	return err
}

func (r *PostgresMessagesRepo) Fetch(ctx context.Context, roomId uuid.UUID, limit int, before time.Time) ([]*models.Message, error) {
	if before.IsZero() {
		before = time.Now()
	}

	query := `
        SELECT id, room_id, sender, content, type, created_at
        FROM messages
        WHERE room_id = $1 AND created_at < $2
        ORDER BY created_at DESC
        LIMIT $3
    `

	rows, err := r.pool.Query(ctx, query, roomId, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*models.Message
	for rows.Next() {
		m := &models.Message{}
		err := rows.Scan(
			&m.ID,
			&m.RoomID,
			&m.Sender,
			&m.Content,
			&m.Type,
			&m.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}

	return messages, nil
}
