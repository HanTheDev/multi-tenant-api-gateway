package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/HanTheDev/multi-tenant-api-gateway/internal/admin"
	"github.com/HanTheDev/multi-tenant-api-gateway/internal/auth"
	"github.com/HanTheDev/multi-tenant-api-gateway/internal/cache"
	"github.com/HanTheDev/multi-tenant-api-gateway/internal/config"
	"github.com/HanTheDev/multi-tenant-api-gateway/internal/db"
	"github.com/HanTheDev/multi-tenant-api-gateway/internal/proxy"
	"github.com/HanTheDev/multi-tenant-api-gateway/internal/ratelimit"
	"github.com/gorilla/mux"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	// Initialize database
	database, err := db.NewDB(cfg.DatabaseURL)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer database.Close()

	// Initialize rate limiter
	limiter, err := ratelimit.NewRateLimiter(cfg.RedisURL)
	if err != nil {
		log.Fatal("Failed to initialize rate limiter:", err)
	}
	defer limiter.Close()

	// Initialize semantic cache
	semanticCache, err := cache.NewSemanticCache(database, cfg.RedisURL, "http://localhost:5000")
	if err != nil {
		log.Fatal("Failed to initialize semantic cache:", err)
	}

	// Initialize router
	router := mux.NewRouter()

	// Auth middleware
	authMiddleware := auth.NewMiddleware(cfg.JWTSecret)

	// Public routes
	router.HandleFunc("/health", healthHandler).Methods("GET")
	router.HandleFunc("/auth/token", tokenHandler(database, cfg.JWTSecret)).Methods("POST")

	// Admin routes (you may want to add admin auth middleware here)
	adminHandler := admin.NewAdminHandler(database)
	adminHandler.RegisterRoutes(router)

	// Protected proxy routes
	proxyHandler := proxy.NewHandler(database, limiter, semanticCache)
	router.PathPrefix("/api/").Handler(
		authMiddleware.Authenticate(proxyHandler),
	)

	// Start server
	log.Printf("Server starting on port %s", cfg.ServerPort)
	log.Printf("Admin API available at /admin/*")
	log.Printf("Proxy API available at /api/*")
	if err := http.ListenAndServe(":"+cfg.ServerPort, router); err != nil {
		log.Fatal("Server failed:", err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "healthy",
		"version": "1.0.0",
	})
}

func tokenHandler(database *db.DB, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			APIKey string `json:"api_key"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("Failed to decode request: %v", err)
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		log.Printf("Looking up tenant with API key: %s", req.APIKey)

		tenant, err := database.GetTenantByAPIKey(r.Context(), req.APIKey)
		if err != nil {
			log.Printf("Tenant lookup failed: %v", err)
			http.Error(w, "Invalid API key", http.StatusUnauthorized)
			return
		}

		log.Printf("Found tenant: %s (ID: %d)", tenant.Name, tenant.ID)

		token, err := auth.GenerateToken(tenant.ID, tenant.APIKey, jwtSecret)
		if err != nil {
			log.Printf("Token generation failed: %v", err)
			http.Error(w, "Failed to generate token", http.StatusInternalServerError)
			return
		}

		log.Printf("Token generated successfully for tenant: %s", tenant.Name)

		json.NewEncoder(w).Encode(map[string]string{
			"token": token,
		})
	}
}
