package proxy

import (
	"bytes"
	"context"
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
		log.Println("❌ Unauthorized: No claims in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	log.Printf("📨 Request from tenant ID: %d, Path: %s", claims.TenantID, r.URL.Path)

	// Get tenant info
	tenant, err := h.db.GetTenantByAPIKey(r.Context(), claims.APIKey)
	if err != nil {
		log.Printf("❌ Tenant lookup failed: %v", err)
		http.Error(w, "Tenant not found", http.StatusNotFound)
		return
	}

	log.Printf("✅ Tenant: %s (Backend: %s)", tenant.Name, tenant.BackendURL)

	// Check rate limit
	allowed, err := h.rateLimiter.Allow(r.Context(), tenant.ID, tenant.RateLimitPerHour)
	if err != nil {
		log.Printf("❌ Rate limit check failed: %v", err)
		http.Error(w, "Rate limit check failed", http.StatusInternalServerError)
		return
	}

	if !allowed {
		log.Printf("🚫 Rate limit exceeded for tenant: %d", tenant.ID)
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// Read request body once and cache it
	var bodyBytes []byte
	if r.Body != nil {
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}

	// Try semantic cache for LLM requests
	if h.isLLMRequest(r) && len(bodyBytes) > 0 {
		prompt := h.extractPromptFromBody(bodyBytes)
		if prompt != "" {
			log.Printf("🔍 Checking cache for prompt: %s", prompt[:min(50, len(prompt))])

			cachedResponse, hit, err := h.semanticCache.GetCachedResponse(r.Context(), tenant.ID, prompt)
			if err == nil && hit {
				log.Printf("✅ 🎯 CACHE HIT for tenant %d", tenant.ID)

				// Write cached response
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Cache-Status", "HIT")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(cachedResponse))

				// Log access with cache hit
				elapsed := time.Since(startTime)
				h.logAccess(r.Context(), tenant.ID, r.URL.Path, r.Method, http.StatusOK, elapsed, r.ContentLength, int64(len(cachedResponse)))
				log.Printf("✅ Request completed (CACHED) in %dms", elapsed.Milliseconds())
				return
			}
			log.Printf("❌ Cache MISS for tenant %d", tenant.ID)
		}
	}

	// Parse backend URL
	backendURL, err := url.Parse(tenant.BackendURL)
	if err != nil {
		log.Printf("❌ Invalid backend URL: %s, error: %v", tenant.BackendURL, err)
		http.Error(w, "Invalid backend URL", http.StatusInternalServerError)
		return
	}

	// Create reverse proxy with better transport settings
	proxy := httputil.NewSingleHostReverseProxy(backendURL)

	// Configure transport with retries and timeouts
	proxy.Transport = &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	}

	// Add timeout context (60 seconds for LLM, 30 for others)
	timeout := 30 * time.Second
	if h.isLLMRequest(r) {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	r = r.WithContext(ctx)

	// Restore body for proxy
	if len(bodyBytes) > 0 {
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}

	// Better error handler
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("❌ Proxy error: %v", err)

		if err == context.DeadlineExceeded {
			http.Error(w, "Backend timeout - request took too long", http.StatusGatewayTimeout)
		} else if strings.Contains(err.Error(), "no such host") {
			http.Error(w, "Backend DNS resolution failed", http.StatusBadGateway)
		} else if strings.Contains(err.Error(), "connection refused") {
			http.Error(w, "Backend connection refused", http.StatusBadGateway)
		} else {
			http.Error(w, "Bad Gateway: "+err.Error(), http.StatusBadGateway)
		}
	}

	// Modify request path
	originalPath := r.URL.Path
	if strings.HasPrefix(originalPath, "/api") {
		r.URL.Path = strings.TrimPrefix(originalPath, "/api")
		log.Printf("🔀 Proxying: %s%s", backendURL.String(), r.URL.Path)
	}

	// Capture response for logging and caching
	recorder := &responseRecorder{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
		body:           &bytes.Buffer{},
		headerWritten:  false,
	}

	// Proxy the request with retry logic
	maxRetries := 2
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("🔄 Retry attempt %d/%d", attempt, maxRetries)
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond) // Exponential backoff

			// Reset body and recorder for retry
			if len(bodyBytes) > 0 {
				r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}

			// Create new recorder for retry
			recorder = &responseRecorder{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				body:           &bytes.Buffer{},
				headerWritten:  false,
			}
		}

		// Try the proxy
		proxy.ServeHTTP(recorder, r)

		// If successful or client error (4xx), don't retry
		if recorder.statusCode >= 200 && recorder.statusCode < 500 {
			break
		}

		lastErr = err

		// Only retry on 5xx errors
		if attempt < maxRetries {
			log.Printf("⚠️  Got %d, will retry...", recorder.statusCode)
		}
	}

	if recorder.statusCode >= 500 {
		log.Printf("❌ All retries failed, last error: %v", lastErr)
	} else {
		log.Printf("✅ Response: %d", recorder.statusCode)
	}

	// Cache successful LLM responses
	if h.isLLMRequest(r) && recorder.statusCode == http.StatusOK && len(bodyBytes) > 0 {
		prompt := h.extractPromptFromBody(bodyBytes)
		if prompt != "" && recorder.body.Len() > 0 {
			go func() {
				ctx := context.Background()
				err := h.semanticCache.StoreCachedResponse(ctx, tenant.ID, prompt, recorder.body.String())
				if err != nil {
					log.Printf("❌ Failed to cache response: %v", err)
				} else {
					log.Printf("✅ Response cached for tenant %d", tenant.ID)
				}
			}()
		}
	}

	// Log access
	elapsed := time.Since(startTime)
	h.logAccess(r.Context(), tenant.ID, originalPath, r.Method, recorder.statusCode, elapsed, r.ContentLength, int64(recorder.size))

	log.Printf("✅ Request completed in %dms", elapsed.Milliseconds())
}

func (h *Handler) isLLMRequest(r *http.Request) bool {
	llmPaths := []string{"/v1/chat/completions", "/v1/completions", "/api/chat", "/llm", "/generate"}
	for _, path := range llmPaths {
		if strings.Contains(r.URL.Path, path) {
			return true
		}
	}
	return false
}

func (h *Handler) extractPromptFromBody(bodyBytes []byte) string {
	var reqBody map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
		return ""
	}

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

func (h *Handler) logAccess(ctx context.Context, tenantID int, endpoint, method string, statusCode int, elapsed time.Duration, reqSize, respSize int64) {
	accessLog := &models.AccessLog{
		TenantID:       tenantID,
		Endpoint:       endpoint,
		Method:         method,
		StatusCode:     statusCode,
		ResponseTimeMs: int(elapsed.Milliseconds()),
		RequestSize:    reqSize,
		ResponseSize:   respSize,
	}
	go h.db.LogAccess(ctx, accessLog)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode    int
	size          int
	body          *bytes.Buffer
	headerWritten bool
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	if !r.headerWritten {
		r.statusCode = statusCode
		r.ResponseWriter.WriteHeader(statusCode)
		r.headerWritten = true
	}
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	size, err := r.ResponseWriter.Write(b)
	r.size += size
	if r.body != nil {
		r.body.Write(b)
	}
	return size, err
}
