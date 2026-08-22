package status

import (
	"encoding/json"
	"net/http"
	"time"
)

func Handler(service string, startedAt time.Time) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status/data", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service":    service,
			"status":     "ok",
			"started_at": startedAt,
		})
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte("# No ServiceLib runtime metrics in the native baseline.\n"))
	})
	return mux
}
