package tasks

import (
	"context"
	"log"
	"time"
	"websocket-challenge/internal/repository"

	"github.com/robfig/cron/v3"
)

type TokenCleaner struct {
	repo *repository.PostgresRefreshTokenRepo
}

func NewTokenCleaner(repo *repository.PostgresRefreshTokenRepo) *TokenCleaner {
	return &TokenCleaner{
		repo: repo,
	}
}

func (t *TokenCleaner) Start() {
	c := cron.New()

	_, err := c.AddFunc("0 3 * * *", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		err := t.repo.DeleteExpiredTokens(ctx)
		if err != nil {
			log.Printf("[WORKER] Token cleanup failed: %v", err)
			return
		}
	})
	if err != nil {
		log.Printf("[WORKER] Error scheduling cron: %v", err)
		return
	}

	c.Start()
}
