// Command recorder stores what each trading session decided, what the gateway
// refused and what reached the broker, and serves that state to the demo page.
package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/config"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/store"
)

func main() {
	cfg := config.Load()
	state := store.NewMemory()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(state.Read()); err != nil {
			log.Printf("recorder: encode state: %v", err)
		}
	})

	log.Printf("recorder: listening on %s", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, mux); err != nil {
		log.Fatalf("recorder: %v", err)
	}
}
