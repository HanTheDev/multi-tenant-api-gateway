package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/HanTheDev/multi-tenant-api-gateway/internal/auth"
	"github.com/HanTheDev/multi-tenant-api-gateway/internal/cache"
	"github.com/HanTheDev/multi-tenant-api-gateway/internal/db"
	"github.com/HanTheDev/multi-tenant-api-gateway/internal/models"
	"github.com/HanTheDev/multi-tenant-api-gateway/internal/ratelimit"
)

type Handler struct {
	db            *db.DB
	rateLimiter   *ratelimit.RateLimiter
	semanticCache *cache.SemanticCache
}

func NewHandler(database *db.DB, limiter *ratelimit.RateLimiter, semCache *cache.SemanticCache) *Handler {
	return &Handler{
		db:            database,
		rateLimiter:   limiter,
		semanticCache: semCache,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	claims, ok := auth.GetTenantFromContext(r.Context())
	if !ok {
		log.Println("Unauthorized: No claims in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	log.Printf("Request from tenant ID: %d, API Key: %s", claims.TenantID, claims.APIKey)

	// Get tenant info
	tenant, err := h.db.GetTenantByAPIKey(r.Context(), claims.APIKey)
	if err != nil {
		log.Printf("Tenant lookup failed: %v", err)
		http.Error(w, "Tenant not found", http.StatusNotFound)
		return
	}

	log.Printf("Tenant found: %s (ID: %d, Backend: %s)", tenant.Name, tenant.ID, tenant.BackendURL)

	// Check rate limit
	allowed, err := h.rateLimiter.Allow(r.Context(), tenant.ID, tenant.RateLimitPerHour)
	if err != nil {
		log.Printf("Rate limit check failed: %v", err)
		http.Error(w, "Rate limit check failed", http.StatusInternalServerError)
		return
	}

	if !allowed {
		log.Printf("Rate limit exceeded for tenant: %d", tenant.ID)
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	log.Printf("Rate limit OK for tenant: %d", tenant.ID)

	// Parse backend URL
	backendURL, err := url.Parse(tenant.BackendURL)
	if err != nil {
		log.Printf("Invalid backend URL: %s, error: %v", tenant.BackendURL, err)
		http.Error(w, "Invalid backend URL", http.StatusInternalServerError)
		return
	}

	log.Printf("Parsed backend URL: %s", backendURL.String())

	// Create reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(backendURL)

	// Add error handler to proxy
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("Proxy error: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}

	// Modify request to include proper path
	originalPath := r.URL.Path
	log.Printf("Original request path: %s", originalPath)

	// Remove /api prefix for backend
	if strings.HasPrefix(originalPath, "/api") {
		r.URL.Path = strings.TrimPrefix(originalPath, "/api")
		log.Printf("Modified request path: %s", r.URL.Path)
	}

	// Capture response for logging
	recorder := &responseRecorder{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
		body:           &bytes.Buffer{},
	}

	log.Printf("Proxying request to: %s%s", backendURL.String(), r.URL.Path)

	// Proxy the request
	proxy.ServeHTTP(recorder, r)

	log.Printf("Response status: %d", recorder.statusCode)

	// Log the request
	elapsed := time.Since(startTime)
	accessLog := &models.AccessLog{
		TenantID:       tenant.ID,
		Endpoint:       originalPath,
		Method:         r.Method,
		StatusCode:     recorder.statusCode,
		ResponseTimeMs: int(elapsed.Milliseconds()),
		RequestSize:    r.ContentLength,
		ResponseSize:   int64(recorder.size),
	}

	go h.db.LogAccess(r.Context(), accessLog)

	log.Printf("Request completed in %dms", elapsed.Milliseconds())
}

// Check if the request is to an LLM endpoint
func (h *Handler) isLLMRequest(r *http.Request) bool {
	// Check if path contains common LLM endpoints
	llmPaths := []string{"/v1/chat/completions", "/v1/completions", "/api/chat", "/llm"}
	for _, path := range llmPaths {
		if strings.Contains(r.URL.Path, path) {
			return true
		}
	}
	return false
}

// Try to get response from semantic cache
func (h *Handler) trySemanticCache(w http.ResponseWriter, r *http.Request, tenantID int) (string, bool) {
	// Read request body to extract prompt
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return "", false
	}

	// Restore body for potential proxy use
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Parse request to get prompt
	var reqBody map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
		return "", false
	}

	// Extract prompt (format varies by API)
	prompt := extractPrompt(reqBody)
	if prompt == "" {
		return "", false
	}

	// Check cache
	cachedResponse, hit, err := h.semanticCache.GetCachedResponse(r.Context(), tenantID, prompt)
	if err != nil || !hit {
		return "", false
	}

	// Write cached response
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "HIT")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(cachedResponse))

	return cachedResponse, true
}

// Cache the LLM response
func (h *Handler) cacheResponse(r *http.Request, tenantID int, responseBody []byte) {
	// Read original request body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return
	}

	var reqBody map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
		return
	}

	prompt := extractPrompt(reqBody)
	if prompt == "" {
		return
	}

	// Store in cache
	h.semanticCache.StoreCachedResponse(r.Context(), tenantID, prompt, string(responseBody))
}

// Extract prompt from various LLM API formats
func extractPrompt(reqBody map[string]interface{}) string {
	// OpenAI format
	if messages, ok := reqBody["messages"].([]interface{}); ok && len(messages) > 0 {
		if lastMsg, ok := messages[len(messages)-1].(map[string]interface{}); ok {
			if content, ok := lastMsg["content"].(string); ok {
				return content
			}
		}
	}

	// Simple prompt format
	if prompt, ok := reqBody["prompt"].(string); ok {
		return prompt
	}

	// Question format
	if question, ok := reqBody["question"].(string); ok {
		return question
	}

	return ""
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	size       int
	body       *bytes.Buffer
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	size, err := r.ResponseWriter.Write(b)
	r.size += size

	// Also write to buffer for caching
	if r.body != nil {
		r.body.Write(b)
	}

	return size, err
}
