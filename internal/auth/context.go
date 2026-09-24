package auth

import "context"

type ctxKey struct{}

type Identity struct {
	UserID    int64
	Email     string
	TokenHash string
}

func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func IdentityFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}
