package models

import "time"

type Tenant struct {
	ID               int       `json:"id"`
	Name             string    `json:"name"`
	APIKey           string    `json:"api_key"`
	RateLimitPerHour int       `json:"rate_limit_per_hour"`
	BackendURL       string    `json:"backend_url"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type AccessLog struct {
	ID             int64     `json:"id"`
	TenantID       int       `json:"tenant_id"`
	Endpoint       string    `json:"endpoint"`
	Method         string    `json:"method"`
	StatusCode     int       `json:"status_code"`
	ResponseTimeMs int       `json:"response_time_ms"`
	RequestSize    int64     `json:"request_size"`
	ResponseSize   int64     `json:"response_size"`
	Timestamp      time.Time `json:"timestamp"`
}

type SemanticCache struct {
	ID              int64     `json:"id"`
	TenantID        int       `json:"tenant_id"`
	PromptHash      string    `json:"prompt_hash"`
	Prompt          string    `json:"prompt"`
	Response        string    `json:"response"`
	EmbeddingStored bool      `json:"embedding_stored"`
	HitCount        int       `json:"hit_count"`
	CreatedAt       time.Time `json:"created_at"`
	LastAccessed    time.Time `json:"last_accessed"`
}
