package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/kevinlin/realdeal-api/internal/config"
	"github.com/kevinlin/realdeal-api/internal/database"
	"github.com/kevinlin/realdeal-api/internal/handlers"
	"github.com/kevinlin/realdeal-api/internal/middleware"
	"github.com/kevinlin/realdeal-api/internal/services"
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

	// Initialize upload service (optional — server starts even if S3 is not configured)
	var uploadSvc services.UploadServiceInterface
	if svc, err := services.NewUploadService(cfg); err != nil {
		log.Printf("Upload service unavailable: %v", err)
	} else {
		uploadSvc = svc
	}

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(db, cfg)
	socialAuthHandler := handlers.NewSocialAuthHandler(db, cfg)
	userHandler := handlers.NewUserHandler(db)
	propertyHandler := handlers.NewPropertyHandler(db)
	favoriteHandler := handlers.NewFavoriteHandler(db)
	uploadHandler := handlers.NewUploadHandler(uploadSvc)
	offerHandler := handlers.NewOfferHandler(db, cfg.PaymentDeadlineHours, cfg.ContractExecutionDeadlineDays)
	viewingHandler := handlers.NewViewingHandler(db)
	trustHandler := handlers.NewTrustHandler(db)
	contractHandler := handlers.NewContractHandler(db)

	// Auth middleware
	authMW := middleware.AuthMiddleware(cfg)

	// API v1 group
	v1 := r.Group("/api/v1")

	// Public config — single source of truth for the client's country dropdown
	v1.GET("/config/countries", handlers.SupportedCountriesHandler)

	// Auth routes (no auth required)
	auth := v1.Group("/auth")
	{
		auth.POST("/signup", authHandler.Signup)
		auth.POST("/signin", authHandler.Signin)
		auth.POST("/signin/apple", socialAuthHandler.SignInWithApple)
		auth.POST("/signin/google", socialAuthHandler.SignInWithGoogle)
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

		// Trust appeal (self-service filing only; resolution is manual ops)
		users.POST("/me/trust-appeal", authMW, trustHandler.FileAppeal)
	}

	// Upload routes
	v1.POST("/upload/presign", authMW, uploadHandler.Presign)

	// Property routes
	properties := v1.Group("/properties")
	{
		properties.GET("", propertyHandler.ListProperties)
		properties.GET("/:id", propertyHandler.GetProperty)
		properties.POST("", authMW, propertyHandler.CreateProperty)
		properties.PUT("/:id", authMW, propertyHandler.UpdateProperty)
		properties.DELETE("/:id", authMW, propertyHandler.DeleteProperty)

		// Offer routes (nested under property)
		properties.POST("/:id/offers", authMW, offerHandler.SubmitOffer)
		properties.GET("/:id/offers", authMW, offerHandler.ListOffers)
		properties.PUT("/:id/offers/:offerId/accept", authMW, offerHandler.AcceptOffer)
		properties.PUT("/:id/offers/:offerId/reject", authMW, offerHandler.RejectOffer)
		properties.DELETE("/:id/offers/:offerId", authMW, offerHandler.WithdrawOffer)
		properties.POST("/:id/offers/:offerId/report-nonpayment", authMW, trustHandler.ReportNonPayment)
		properties.POST("/:id/offers/:offerId/report-seller", authMW, trustHandler.ReportSeller)

		// Viewing slot routes (nested under property)
		properties.POST("/:id/viewing-slots", authMW, viewingHandler.CreateSlot)
		properties.GET("/:id/viewing-slots", viewingHandler.ListSlots)
		properties.DELETE("/:id/viewing-slots/:slotId", authMW, viewingHandler.DeleteSlot)
		properties.POST("/:id/viewing-slots/:slotId/requests", authMW, viewingHandler.RequestViewing)

		// Viewing request routes (nested under property, seller-facing)
		properties.GET("/:id/viewing-requests", authMW, viewingHandler.ListRequests)
		properties.PUT("/:id/viewing-requests/:requestId/accept", authMW, viewingHandler.AcceptRequest)
		properties.PUT("/:id/viewing-requests/:requestId/decline", authMW, viewingHandler.DeclineRequest)

		// Contract routes (nested under property/offer) — the contract is
		// created automatically by AcceptOffer; there is no create endpoint.
		properties.GET("/:id/offers/:offerId/contract", authMW, contractHandler.GetContract)
		properties.PUT("/:id/offers/:offerId/contract/terms", authMW, contractHandler.ProposeTerms)
		properties.POST("/:id/offers/:offerId/contract/agree-terms", authMW, contractHandler.AgreeTerms)
		properties.POST("/:id/offers/:offerId/contract/sign", authMW, contractHandler.Sign)
		properties.POST("/:id/offers/:offerId/contract/cancel", authMW, contractHandler.Cancel)
	}

	// Seller's own listings (all non-deleted statuses — active/pending/sold)
	users.GET("/me/listings", authMW, propertyHandler.ListMyListings)

	// Buyer's own offers
	users.GET("/me/offers", authMW, offerHandler.ListMyOffers)

	// Buyer's own viewing requests
	users.GET("/me/viewing-requests", authMW, viewingHandler.ListMyRequests)

	// Caller's contracts (both buyer and seller side)
	users.GET("/me/contracts", authMW, contractHandler.ListMyContracts)

	// Cancel a viewing request (buyer-owner-only)
	v1.DELETE("/viewing-requests/:requestId", authMW, viewingHandler.CancelRequest)

	// Start server
	log.Printf("Starting server on :%s (env: %s)", cfg.Port, cfg.Env)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
