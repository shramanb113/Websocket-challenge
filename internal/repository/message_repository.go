package repository

import (
	"context"
	"fmt"
	"log"
	"time"
	"websocket-challenge/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MessageRepo interface {
	Save(ctx context.Context, message *models.Message) error
	Fetch(ctx context.Context, roomId string, userName string, limit int, before time.Time) ([]*models.Message, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status models.MessageStatus) error
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
	// Added 'status' to the column list and values
	query := `
        INSERT INTO messages (id, room_id, sender_name, target_user, content, msg_type, created_at, status)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO NOTHING
    `

	_, err := r.pool.Exec(ctx, query,
		m.ID,
		m.RoomID,
		m.Sender,
		m.Target,
		m.Content,
		m.Type,
		m.Timestamp,
		m.Status,
	)

	if err != nil {
		log.Printf("[REPO ERROR] Failed to save message %s from %s: %v", m.ID, m.Sender, err)
		return err
	}

	return nil
}

func (r *PostgresMessagesRepo) Fetch(ctx context.Context, roomId string, userName string, limit int, before time.Time) ([]*models.Message, error) {
	if before.IsZero() {
		before = time.Now()
	}

	query := `
        SELECT id, room_id, sender_name, target_user, content, msg_type, created_at,status
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
			&m.Status,
		)
		if err != nil {
			log.Printf("[REPO ERROR] Scan failed: %v", err)
			return nil, err
		}
		messages = append(messages, m)
	}

	return messages, nil
}

func (r *PostgresMessagesRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status models.MessageStatus) error {

	query := `
		UPDATE messages
		SET status = $2
		WHERE id = $1 and status < $2`

	tag, err := r.pool.Exec(ctx, query, id, int(status))

	if err != nil {
		log.Printf("[REPO ERROR] Failed to update status for message %s: %v", id, err)
		return fmt.Errorf("database update failed: %w", err)
	}

	if tag.RowsAffected() == 0 {
		log.Printf("[REPO INFO] No rows updated for message %s (might be already updated or ID missing)", id)
	}

	return nil

}
