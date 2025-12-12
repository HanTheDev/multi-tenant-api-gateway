package db

import (
	"context"

	"github.com/HanTheDev/multi-tenant-api-gateway/internal/models"
)

func (db *DB) GetTenantByAPIKey(ctx context.Context, apiKey string) (*models.Tenant, error) {
	query := `
        SELECT id, name, api_key, rate_limit_per_hour, backend_url, created_at, updated_at
        FROM tenants
        WHERE api_key = $1
    `

	var tenant models.Tenant
	err := db.Pool.QueryRow(ctx, query, apiKey).Scan(
		&tenant.ID,
		&tenant.Name,
		&tenant.APIKey,
		&tenant.RateLimitPerHour,
		&tenant.BackendURL,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &tenant, nil
}

func (db *DB) LogAccess(ctx context.Context, log *models.AccessLog) error {
	query := `
        INSERT INTO access_logs (tenant_id, endpoint, method, status_code, response_time_ms, request_size, response_size)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
    `

	_, err := db.Pool.Exec(ctx, query,
		log.TenantID,
		log.Endpoint,
		log.Method,
		log.StatusCode,
		log.ResponseTimeMs,
		log.RequestSize,
		log.ResponseSize,
	)

	return err
}

func (db *DB) GetCachedResponse(ctx context.Context, tenantID int, promptHash string) (*models.SemanticCache, error) {
	query := `
        UPDATE semantic_cache
        SET hit_count = hit_count + 1, last_accessed = NOW()
        WHERE tenant_id = $1 AND prompt_hash = $2
        RETURNING id, tenant_id, prompt_hash, prompt, response, embedding_stored, hit_count, created_at, last_accessed
    `

	var cache models.SemanticCache
	err := db.Pool.QueryRow(ctx, query, tenantID, promptHash).Scan(
		&cache.ID,
		&cache.TenantID,
		&cache.PromptHash,
		&cache.Prompt,
		&cache.Response,
		&cache.EmbeddingStored,
		&cache.HitCount,
		&cache.CreatedAt,
		&cache.LastAccessed,
	)

	if err != nil {
		return nil, err
	}

	return &cache, nil
}

func (db *DB) StoreCachedResponse(ctx context.Context, cache *models.SemanticCache) error {
	query := `
        INSERT INTO semantic_cache (tenant_id, prompt_hash, prompt, response, embedding_stored)
        VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (prompt_hash) DO UPDATE
        SET response = EXCLUDED.response, last_accessed = NOW()
    `

	_, err := db.Pool.Exec(ctx, query,
		cache.TenantID,
		cache.PromptHash,
		cache.Prompt,
		cache.Response,
		cache.EmbeddingStored,
	)

	return err
}
