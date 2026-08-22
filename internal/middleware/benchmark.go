package middleware

import (
	"context"
	"net/http"

	"google.golang.org/grpc/stats"
)

// HTTP preserves the same per-request middleware boundary used by the
// framework benchmark while deliberately recording nothing.
func HTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

// GRPCStats preserves gRPC client/server stats callbacks without retaining
// observations, matching a Noop metrics engine.
type GRPCStats struct{}

func (GRPCStats) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context   { return ctx }
func (GRPCStats) HandleRPC(context.Context, stats.RPCStats)                         {}
func (GRPCStats) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context { return ctx }
func (GRPCStats) HandleConn(context.Context, stats.ConnStats)                       {}
