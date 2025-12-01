// Package model contains domain models used across routers and DB layers.
package model

import "time"

// Post represents a social post entity as stored in the database.
// Field types align with Postgres schema: BIGINT -> int64, TIMESTAMP -> time.Time.
type Post struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	Content   string    `json:"content"`
}
