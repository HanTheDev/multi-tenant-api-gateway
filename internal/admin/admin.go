package admin

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"crypto/rand"
	"encoding/hex"

	"github.com/HanTheDev/multi-tenant-api-gateway/internal/db"
	"github.com/HanTheDev/multi-tenant-api-gateway/internal/models"
	"github.com/gorilla/mux"
)

type AdminHandler struct {
	db *db.DB
}

func NewAdminHandler(database *db.DB) *AdminHandler {
	return &AdminHandler{db: database}
}

func (h *AdminHandler) RegisterRoutes(router *mux.Router) {
	// Tenant management
	router.HandleFunc("/admin/tenants", h.ListTenants).Methods("GET")
	router.HandleFunc("/admin/tenants", h.CreateTenant).Methods("POST")
	router.HandleFunc("/admin/tenants/{id}", h.GetTenant).Methods("GET")
	router.HandleFunc("/admin/tenants/{id}", h.UpdateTenant).Methods("PUT")
	router.HandleFunc("/admin/tenants/{id}", h.DeleteTenant).Methods("DELETE")
	router.HandleFunc("/admin/tenants/{id}/rotate-key", h.RotateAPIKey).Methods("POST")

	// Analytics
	router.HandleFunc("/admin/tenants/{id}/analytics", h.GetAnalytics).Methods("GET")
	router.HandleFunc("/admin/cache/stats", h.GetCacheStats).Methods("GET")
}

func (h *AdminHandler) CreateTenant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name             string `json:"name"`
		BackendURL       string `json:"backend_url"`
		RateLimitPerHour int    `json:"rate_limit_per_hour"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Validate inputs
	if req.Name == "" || req.BackendURL == "" {
		http.Error(w, "Name and backend_url are required", http.StatusBadRequest)
		return
	}

	if req.RateLimitPerHour <= 0 {
		req.RateLimitPerHour = 1000 // Default
	}

	// Generate API key
	apiKey, err := generateAPIKey()
	if err != nil {
		http.Error(w, "Failed to generate API key", http.StatusInternalServerError)
		return
	}

	tenant := &models.Tenant{
		Name:             req.Name,
		APIKey:           apiKey,
		BackendURL:       req.BackendURL,
		RateLimitPerHour: req.RateLimitPerHour,
	}

	if err := h.db.CreateTenant(r.Context(), tenant); err != nil {
		log.Printf("Failed to create tenant: %v", err)
		http.Error(w, "Failed to create tenant", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(tenant)
}

func (h *AdminHandler) ListTenants(w http.ResponseWriter, r *http.Request) {
	tenants, err := h.db.ListTenants(r.Context())
	if err != nil {
		http.Error(w, "Failed to list tenants", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tenants)
}

func (h *AdminHandler) GetTenant(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid tenant ID", http.StatusBadRequest)
		return
	}

	tenant, err := h.db.GetTenantByID(r.Context(), id)
	if err != nil {
		http.Error(w, "Tenant not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tenant)
}

func (h *AdminHandler) UpdateTenant(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid tenant ID", http.StatusBadRequest)
		return
	}

	var updates struct {
		Name             *string `json:"name"`
		BackendURL       *string `json:"backend_url"`
		RateLimitPerHour *int    `json:"rate_limit_per_hour"`
	}

	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if err := h.db.UpdateTenant(r.Context(), id, updates); err != nil {
		http.Error(w, "Failed to update tenant", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func (h *AdminHandler) DeleteTenant(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid tenant ID", http.StatusBadRequest)
		return
	}

	if err := h.db.DeleteTenant(r.Context(), id); err != nil {
		http.Error(w, "Failed to delete tenant", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminHandler) RotateAPIKey(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid tenant ID", http.StatusBadRequest)
		return
	}

	newAPIKey, err := generateAPIKey()
	if err != nil {
		http.Error(w, "Failed to generate API key", http.StatusInternalServerError)
		return
	}

	if err := h.db.RotateAPIKey(r.Context(), id, newAPIKey); err != nil {
		http.Error(w, "Failed to rotate API key", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"api_key": newAPIKey,
		"status":  "rotated",
	})
}

func (h *AdminHandler) GetAnalytics(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tenantID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid tenant ID", http.StatusBadRequest)
		return
	}

	// Get query params for time range
	from := r.URL.Query().Get("from") // e.g., "2024-01-01"
	to := r.URL.Query().Get("to")

	stats, err := h.db.GetTenantAnalytics(r.Context(), tenantID, from, to)
	if err != nil {
		http.Error(w, "Failed to get analytics", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (h *AdminHandler) GetCacheStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.db.GetCacheStats(r.Context())
	if err != nil {
		http.Error(w, "Failed to get cache stats", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func generateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
