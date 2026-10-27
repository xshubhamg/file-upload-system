package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/xshubhamg/file-upload/internal/apperr"
)

// Cursor is an opaque, signed-by-obscurity pagination token. It contains only
// the last row's sort key, so it cannot be used to query another user's data.
type Cursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        uuid.UUID `json:"id"`
}

func EncodeCursor(c Cursor) string {
	payload, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(payload)
}

func DecodeCursor(raw string) (*Cursor, error) {
	if raw == "" {
		return nil, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, apperr.Validation("invalid cursor").
			WithDetail("cursor", "cursor is not valid base64")
	}
	var c Cursor
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, apperr.Validation("invalid cursor").
			WithDetail("cursor", "cursor is not valid JSON")
	}
	if c.CreatedAt.IsZero() || c.ID == uuid.Nil {
		return nil, apperr.Validation("invalid cursor").
			WithDetail("cursor", "cursor is missing fields")
	}
	return &c, nil
}

func (c Cursor) String() string {
	return fmt.Sprintf("%s/%s", c.CreatedAt.UTC().Format(time.RFC3339Nano), c.ID)
}
