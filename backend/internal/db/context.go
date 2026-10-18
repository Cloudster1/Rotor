package db

import (
	"context"

	"github.com/google/uuid"
)

type userKey struct{}

// UserVar is the setting the policies read through current_actor_id(): whom the transaction acts for.
const UserVar = "app.user_id"

// WithUser names the person the transactions made with ctx act for, so row
// level security holds them to that person's permissions.
func WithUser(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, userKey{}, id)
}

// UserFrom is the person WithUser named, if any.
func UserFrom(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(userKey{}).(uuid.UUID)
	return id, ok && id != uuid.Nil
}
