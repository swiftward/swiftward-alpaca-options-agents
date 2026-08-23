// Command runner starts trading sessions on a schedule and records why each one
// ran. No session is ever started by a person.
package main

import (
	"log"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/config"
)

func main() {
	cfg := config.Load()
	log.Printf("runner: recorder at %s, gateway at %q", cfg.RecorderURL, cfg.GatewayURL)
	log.Println("runner: no schedule configured yet")
}
