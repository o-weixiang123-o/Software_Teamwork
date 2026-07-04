package adapter

import (
	"context"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/vendorclient"
)

type requestIDKey struct{}

func contextWithRequestID(ctx context.Context, requestID string) context.Context {
	ctx = context.WithValue(ctx, requestIDKey{}, requestID)
	return vendorclient.ContextWithRequestID(ctx, requestID)
}

func requestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}
