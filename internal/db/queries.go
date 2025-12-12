package db

import (
	"context"
	"time"

	"github.com/HanTheDev/multi-tenant-api-gateway/internal/models"
)

// ============ Existing Methods ============

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

// ============ NEW Admin Methods ============

func (db *DB) CreateTenant(ctx context.Context, tenant *models.Tenant) error {
	query := `
        INSERT INTO tenants (name, api_key, rate_limit_per_hour, backend_url)
        VALUES ($1, $2, $3, $4)
        RETURNING id, created_at, updated_at
    `

	err := db.Pool.QueryRow(ctx, query,
		tenant.Name,
		tenant.APIKey,
		tenant.RateLimitPerHour,
		tenant.BackendURL,
	).Scan(&tenant.ID, &tenant.CreatedAt, &tenant.UpdatedAt)

	return err
}

func (db *DB) ListTenants(ctx context.Context) ([]models.Tenant, error) {
	query := `
        SELECT id, name, api_key, rate_limit_per_hour, backend_url, created_at, updated_at
        FROM tenants
        ORDER BY created_at DESC
    `

	rows, err := db.Pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tenants []models.Tenant
	for rows.Next() {
		var tenant models.Tenant
		err := rows.Scan(
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
		tenants = append(tenants, tenant)
	}

	return tenants, nil
}

func (db *DB) GetTenantByID(ctx context.Context, id int) (*models.Tenant, error) {
	query := `
        SELECT id, name, api_key, rate_limit_per_hour, backend_url, created_at, updated_at
        FROM tenants
        WHERE id = $1
    `

	var tenant models.Tenant
	err := db.Pool.QueryRow(ctx, query, id).Scan(
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

func (db *DB) UpdateTenant(ctx context.Context, id int, updates interface{}) error {
	// Type assertion to map
	updateMap, ok := updates.(map[string]interface{})
	if !ok {
		// If it's a struct, convert to map manually
		type UpdateStruct struct {
			Name             *string `json:"name"`
			BackendURL       *string `json:"backend_url"`
			RateLimitPerHour *int    `json:"rate_limit_per_hour"`
		}

		if updateStruct, ok := updates.(UpdateStruct); ok {
			updateMap = make(map[string]interface{})
			if updateStruct.Name != nil {
				updateMap["name"] = *updateStruct.Name
			}
			if updateStruct.BackendURL != nil {
				updateMap["backend_url"] = *updateStruct.BackendURL
			}
			if updateStruct.RateLimitPerHour != nil {
				updateMap["rate_limit_per_hour"] = *updateStruct.RateLimitPerHour
			}
		}
	}

	// Build dynamic update query
	query := "UPDATE tenants SET updated_at = NOW()"
	args := []interface{}{}
	argCount := 1

	if name, ok := updateMap["name"]; ok {
		query += ", name = $" + string(rune(argCount+'0'))
		args = append(args, name)
		argCount++
	}
	if backendURL, ok := updateMap["backend_url"]; ok {
		query += ", backend_url = $" + string(rune(argCount+'0'))
		args = append(args, backendURL)
		argCount++
	}
	if rateLimit, ok := updateMap["rate_limit_per_hour"]; ok {
		query += ", rate_limit_per_hour = $" + string(rune(argCount+'0'))
		args = append(args, rateLimit)
		argCount++
	}

	query += " WHERE id = $" + string(rune(argCount+'0'))
	args = append(args, id)

	_, err := db.Pool.Exec(ctx, query, args...)
	return err
}

func (db *DB) DeleteTenant(ctx context.Context, id int) error {
	query := `DELETE FROM tenants WHERE id = $1`
	_, err := db.Pool.Exec(ctx, query, id)
	return err
}

func (db *DB) RotateAPIKey(ctx context.Context, id int, newAPIKey string) error {
	query := `
        UPDATE tenants
        SET api_key = $1, updated_at = NOW()
        WHERE id = $2
    `
	_, err := db.Pool.Exec(ctx, query, newAPIKey, id)
	return err
}

func (db *DB) GetTenantAnalytics(ctx context.Context, tenantID int, from, to string) (map[string]interface{}, error) {
	// Default time range if not provided
	if from == "" {
		from = time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	}
	if to == "" {
		to = time.Now().Format("2006-01-02")
	}

	// Get request count and average response time
	statsQuery := `
        SELECT 
            COUNT(*) as total_requests,
            AVG(response_time_ms) as avg_response_time,
            SUM(request_size) as total_request_size,
            SUM(response_size) as total_response_size,
            COUNT(CASE WHEN status_code >= 200 AND status_code < 300 THEN 1 END) as success_count,
            COUNT(CASE WHEN status_code >= 400 THEN 1 END) as error_count
        FROM access_logs
        WHERE tenant_id = $1 
        AND timestamp >= $2::timestamp 
        AND timestamp <= $3::timestamp
    `

	var stats struct {
		TotalRequests     int64
		AvgResponseTime   float64
		TotalRequestSize  int64
		TotalResponseSize int64
		SuccessCount      int64
		ErrorCount        int64
	}

	err := db.Pool.QueryRow(ctx, statsQuery, tenantID, from, to).Scan(
		&stats.TotalRequests,
		&stats.AvgResponseTime,
		&stats.TotalRequestSize,
		&stats.TotalResponseSize,
		&stats.SuccessCount,
		&stats.ErrorCount,
	)
	if err != nil {
		return nil, err
	}

	// Get top endpoints
	endpointsQuery := `
        SELECT endpoint, COUNT(*) as count
        FROM access_logs
        WHERE tenant_id = $1 
        AND timestamp >= $2::timestamp 
        AND timestamp <= $3::timestamp
        GROUP BY endpoint
        ORDER BY count DESC
        LIMIT 10
    `

	rows, err := db.Pool.Query(ctx, endpointsQuery, tenantID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	topEndpoints := []map[string]interface{}{}
	for rows.Next() {
		var endpoint string
		var count int64
		if err := rows.Scan(&endpoint, &count); err != nil {
			continue
		}
		topEndpoints = append(topEndpoints, map[string]interface{}{
			"endpoint": endpoint,
			"count":    count,
		})
	}

	return map[string]interface{}{
		"total_requests":       stats.TotalRequests,
		"avg_response_time_ms": stats.AvgResponseTime,
		"total_request_size":   stats.TotalRequestSize,
		"total_response_size":  stats.TotalResponseSize,
		"success_count":        stats.SuccessCount,
		"error_count":          stats.ErrorCount,
		"success_rate":         float64(stats.SuccessCount) / float64(stats.TotalRequests) * 100,
		"top_endpoints":        topEndpoints,
		"time_range": map[string]string{
			"from": from,
			"to":   to,
		},
	}, nil
}

func (db *DB) GetCacheStats(ctx context.Context) (map[string]interface{}, error) {
	query := `
        SELECT 
            COUNT(*) as total_cached,
            SUM(hit_count) as total_hits,
            AVG(hit_count) as avg_hits_per_entry,
            COUNT(CASE WHEN embedding_stored = true THEN 1 END) as embeddings_stored
        FROM semantic_cache
    `

	var stats struct {
		TotalCached      int64
		TotalHits        int64
		AvgHitsPerEntry  float64
		EmbeddingsStored int64
	}

	err := db.Pool.QueryRow(ctx, query).Scan(
		&stats.TotalCached,
		&stats.TotalHits,
		&stats.AvgHitsPerEntry,
		&stats.EmbeddingsStored,
	)
	if err != nil {
		return nil, err
	}

	// Get top cached queries
	topQuery := `
        SELECT prompt, hit_count, last_accessed
        FROM semantic_cache
        ORDER BY hit_count DESC
        LIMIT 10
    `

	rows, err := db.Pool.Query(ctx, topQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	topQueries := []map[string]interface{}{}
	for rows.Next() {
		var prompt string
		var hitCount int64
		var lastAccessed time.Time
		if err := rows.Scan(&prompt, &hitCount, &lastAccessed); err != nil {
			continue
		}
		topQueries = append(topQueries, map[string]interface{}{
			"prompt":        prompt,
			"hit_count":     hitCount,
			"last_accessed": lastAccessed,
		})
	}

	return map[string]interface{}{
		"total_cached":       stats.TotalCached,
		"total_hits":         stats.TotalHits,
		"avg_hits_per_entry": stats.AvgHitsPerEntry,
		"embeddings_stored":  stats.EmbeddingsStored,
		"top_cached_queries": topQueries,
	}, nil
}
