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

func main() {
	// Load .env file in development (ignored in production where env vars are set directly)
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, reading from environment")
	}

	// Load and validate config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	// Connect to the database
	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("Could not connect to database: %v", err)
	}

	// Set Gin mode
	if cfg.Env != "development" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create router
	r := gin.New()
	r.Use(middleware.Logger())
	r.Use(gin.Recovery())

	// Health check
	healthHandler := handlers.NewHealthHandler(db)
	r.GET("/health", healthHandler.Health)

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(db, cfg)
	userHandler := handlers.NewUserHandler(db)
	propertyHandler := handlers.NewPropertyHandler(db)
	favoriteHandler := handlers.NewFavoriteHandler(db)

	// Auth middleware
	authMW := middleware.AuthMiddleware(cfg)

	// API v1 group
	v1 := r.Group("/api/v1")

	// Auth routes (no auth required)
	auth := v1.Group("/auth")
	{
		auth.POST("/signup", authHandler.Signup)
		auth.POST("/signin", authHandler.Signin)
		auth.POST("/refresh", authHandler.Refresh)
		auth.POST("/signout", authMW, authHandler.Signout)
	}

	// User routes
	users := v1.Group("/users")
	{
		users.GET("/me", authMW, userHandler.GetMe)
		users.GET("/:id", userHandler.GetUser)
		users.PUT("/:id", authMW, userHandler.UpdateUser)
		users.DELETE("/:id", authMW, userHandler.DeleteUser)

		// Favorites
		users.GET("/:id/favorites", authMW, favoriteHandler.ListFavorites)
		users.POST("/:id/favorites", authMW, favoriteHandler.AddFavorite)
		users.DELETE("/:id/favorites/:propertyId", authMW, favoriteHandler.RemoveFavorite)
	}

	// Property routes
	properties := v1.Group("/properties")
	{
		properties.GET("", propertyHandler.ListProperties)
		properties.GET("/:id", propertyHandler.GetProperty)
		properties.POST("", authMW, propertyHandler.CreateProperty)
		properties.PUT("/:id", authMW, propertyHandler.UpdateProperty)
		properties.DELETE("/:id", authMW, propertyHandler.DeleteProperty)
	}

	// Start server
	log.Printf("Starting server on :%s (env: %s)", cfg.Port, cfg.Env)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
