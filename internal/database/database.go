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
		&models.TrustEvent{},
		&models.TrustAppeal{},
	); err != nil {
		return nil, fmt.Errorf("auto-migration failed: %w", err)
	}

	// Enforce one accepted viewing request per slot at the database level.
	// The handler already checks-then-writes, but that has a read-then-write
	// race under concurrent accepts on the same (last) slot; this partial
	// unique index is the hard backstop. Partial indexes can't be expressed
	// via GORM struct tags, so it's created with a raw statement after
	// AutoMigrate. IF NOT EXISTS keeps this idempotent across restarts.
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_viewing_requests_one_accepted_per_slot ON viewing_requests (slot_id) WHERE status = 'accepted'`).Error; err != nil {
		return nil, fmt.Errorf("failed to create viewing_requests accepted-slot index: %w", err)
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
