package repository

import (
	"context"
	"log"
	"time"
	"websocket-challenge/internal/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

type MessageRepo interface {
	Save(ctx context.Context, message *models.Message) error
	Fetch(ctx context.Context, roomId string, userName string, limit int, before time.Time) ([]*models.Message, error)
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
        INSERT INTO messages (room_id, sender_name, target_user, content, msg_type, created_at)
        VALUES ($1, $2, $3, $4, $5, $6)
    `

	_, err := r.pool.Exec(ctx, query,
		m.RoomID,
		m.Sender,
		m.Target,
		m.Content,
		m.Type,
		m.Timestamp,
	)

	if err != nil {
		log.Printf("[REPO ERROR] Failed to save message from %s in room %s: %v", m.Sender, m.RoomID, err)
		return err
	}

	return nil
}

func (r *PostgresMessagesRepo) Fetch(ctx context.Context, roomId string, userName string, limit int, before time.Time) ([]*models.Message, error) {
	if before.IsZero() {
		before = time.Now()
	}

	query := `
        SELECT id, room_id, sender_name, target_user, content, msg_type, created_at
        FROM messages
        WHERE room_id = $1 
          AND created_at < $2
          AND (msg_type != 'private' OR sender_name = $3 OR target_user = $3)
        ORDER BY created_at DESC
        LIMIT $4
    `

	rows, err := r.pool.Query(ctx, query, roomId, before, userName, limit)
	if err != nil {
		log.Printf("[REPO ERROR] Fetch failed for room %s: %v", roomId, err)
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
			&m.Target,
			&m.Content,
			&m.Type,
			&m.Timestamp,
		)
		if err != nil {
			log.Printf("[REPO ERROR] Scan failed: %v", err)
			return nil, err
		}
		messages = append(messages, m)
	}

	return messages, nil
}
