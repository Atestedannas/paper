package main

import (
	"encoding/json"
	"flag"
	"log"

	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
)

func main() {
	apply := flag.Bool("apply", false, "execute legacy table decommission")
	flag.Parse()

	cfg, err := config.LoadConfig(".env")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if err := database.InitDatabase(cfg); err != nil {
		log.Fatalf("Failed to init database: %v", err)
	}

	status := database.GetRBACHealthStatus(cfg.RBAC.Model)
	payload, _ := json.MarshalIndent(status, "", "  ")
	log.Printf("RBAC health:\n%s", string(payload))

	if !*apply {
		log.Println("Dry-run mode. Use --apply during low-traffic window.")
		return
	}

	if !status.LegacyTablesPresent {
		log.Println("Legacy tables already decommissioned. Nothing to do.")
		return
	}

	if !status.LegacyDecommissionReady {
		log.Fatalf("Legacy decommission blocked: %v", status.LegacyDecommissionBlockers)
	}

	if err := database.DecommissionLegacyAuthorityTables(); err != nil {
		log.Fatalf("Decommission failed: %v", err)
	}

	after := database.GetRBACHealthStatus(cfg.RBAC.Model)
	afterPayload, _ := json.MarshalIndent(after, "", "  ")
	log.Printf("Decommission completed. Current RBAC health:\n%s", string(afterPayload))
}
