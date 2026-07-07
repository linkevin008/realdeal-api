package database

import (
	"fmt"
	"log"

	"github.com/kevinlin/realdeal-api/internal/config"
	"github.com/kevinlin/realdeal-api/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connect opens a connection to PostgreSQL and returns a GORM DB instance.
// It also enables the pgcrypto extension and runs AutoMigrate to keep the schema
// in sync with the models.
func Connect(cfg *config.Config) (*gorm.DB, error) {
	logLevel := logger.Silent
	if cfg.Env == "development" {
		logLevel = logger.Info // logs all SQL queries in dev
	}

	db, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Enable pgcrypto for gen_random_uuid()
	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS pgcrypto").Error; err != nil {
		return nil, fmt.Errorf("failed to enable pgcrypto extension: %w", err)
	}

	// AutoMigrate in dependency order
	if err := db.AutoMigrate(
		&models.User{},
		&models.Property{},
		&models.PropertyImage{},
		&models.Favorite{},
		&models.Offer{},
		&models.ViewingSlot{},
		&models.ViewingRequest{},
	); err != nil {
		return nil, fmt.Errorf("auto-migration failed: %w", err)
	}

	log.Println("Database connection established and migrations applied")
	return db, nil
}

// ConnectReadOnly opens a connection for read-side services (e.g. lookup) that
// query another service's database. It runs no migrations and creates no
// extensions — the owning service manages the schema, and the read-only DB user
// couldn't perform either anyway.
func ConnectReadOnly(cfg *config.Config) (*gorm.DB, error) {
	logLevel := logger.Silent
	if cfg.Env == "development" {
		logLevel = logger.Info
	}

	db, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	log.Println("Read-only database connection established")
	return db, nil
}
