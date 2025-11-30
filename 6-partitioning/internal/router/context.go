package router

import (
	"context"
	"time"
)

type ctxKey string

// CtxCutoffKey carries a time.Time cutoff for created_at filtering.
const CtxCutoffKey ctxKey = "cutoffTime"

func GetCutoff(ctx context.Context) (time.Time, bool) {
	v := ctx.Value(CtxCutoffKey)
	if v == nil {
		return time.Time{}, false
	}
	t, ok := v.(time.Time)
	return t, ok
}
