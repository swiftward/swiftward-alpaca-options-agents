// Package api serves the read side: the JSON the demo page reads and the built
// page itself. It never reaches the broker and never decides anything.
package api

import (
	"encoding/json"
	"net/http"
	"os"

	"go.uber.org/zap"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/store"
)

// Handler builds the read-side routes. webDir is the directory holding the built
// page; when it is empty only the JSON routes are served, and the log says so,
// because a page served from nowhere would look like a broken deployment.
func Handler(state *store.Memory, webDir string, log *zap.Logger) (http.Handler, error) {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("GET /state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(state.Read()); err != nil {
			log.Error("encode state", zap.Error(err))
		}
	})

	if webDir == "" {
		log.Info("no WEB_DIR set: serving JSON only")
		return mux, nil
	}
	if _, err := os.Stat(webDir); err != nil {
		return nil, err
	}
	mux.Handle("GET /", http.FileServer(http.Dir(webDir)))
	log.Info("serving the built page", zap.String("web_dir", webDir))

	return mux, nil
}
