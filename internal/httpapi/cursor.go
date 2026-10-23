package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/souruking/route-planner/internal/store"
)

// cursorJSON is the cursor's wire format before base64 encoding.
// Clients treat the encoded string as opaque, so this can change freely.
type cursorJSON struct {
	CreatedAt time.Time `json:"t"`
	ID        uuid.UUID `json:"id"`
}

var errBadCursor = errors.New("invalid cursor")

func encodeCursor(c store.Cursor) string {
	// Marshal can't fail for these field types.
	b, _ := json.Marshal(cursorJSON{CreatedAt: c.CreatedAt.UTC(), ID: c.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (store.Cursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return store.Cursor{}, errBadCursor
	}

	var c cursorJSON
	if err := json.Unmarshal(b, &c); err != nil {
		return store.Cursor{}, errBadCursor
	}
	if c.CreatedAt.IsZero() || c.ID == uuid.Nil {
		return store.Cursor{}, errBadCursor
	}

	return store.Cursor{CreatedAt: c.CreatedAt, ID: c.ID}, nil
}
