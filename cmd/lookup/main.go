package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/kevinlin/realdeal-api/internal/config"
	"github.com/kevinlin/realdeal-api/internal/database"
	"github.com/kevinlin/realdeal-api/internal/handlers"
	"github.com/kevinlin/realdeal-api/internal/middleware"
)

// lookup is the read-side search service. It connects to core's database as a
// SELECT-only user (lookup_ro) and never migrates or writes — core owns the
// schema. Locally it runs as its own container behind the gateway; in AWS it
// maps to its own ECS service behind an ALB path rule (/api/v1/search/*).
func main() {
	// Load .env file in development (ignored in production where env vars are set directly)
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, reading from environment")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	db, err := database.ConnectReadOnly(cfg)
	if err != nil {
		log.Fatalf("Could not connect to database: %v", err)
	}

	if cfg.Env != "development" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(middleware.Logger())
	r.Use(gin.Recovery())

	healthHandler := handlers.NewHealthHandler(db)
	r.GET("/health", healthHandler.Health)

	searchHandler := handlers.NewSearchHandler(db)
	v1 := r.Group("/api/v1")
	v1.GET("/search/properties", searchHandler.SearchProperties)

	log.Printf("Starting lookup service on :%s (env: %s)", cfg.Port, cfg.Env)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
