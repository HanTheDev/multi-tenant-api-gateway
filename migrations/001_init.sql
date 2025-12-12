-- Tenants table
CREATE TABLE tenants (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    api_key VARCHAR(255) UNIQUE NOT NULL,
    rate_limit_per_hour INTEGER DEFAULT 1000,
    backend_url VARCHAR(500) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Access logs table
CREATE TABLE access_logs (
    id BIGSERIAL PRIMARY KEY,
    tenant_id INTEGER REFERENCES tenants(id) ON DELETE CASCADE,
    endpoint VARCHAR(500) NOT NULL,
    method VARCHAR(10) NOT NULL,
    status_code INTEGER,
    response_time_ms INTEGER,
    request_size BIGINT,
    response_size BIGINT,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_access_logs_tenant_id ON access_logs(tenant_id);
CREATE INDEX idx_access_logs_timestamp ON access_logs(timestamp);

-- Semantic cache table
CREATE TABLE semantic_cache (
    id BIGSERIAL PRIMARY KEY,
    tenant_id INTEGER REFERENCES tenants(id) ON DELETE CASCADE,
    prompt_hash VARCHAR(64) UNIQUE NOT NULL,
    prompt TEXT NOT NULL,
    response TEXT NOT NULL,
    embedding_stored BOOLEAN DEFAULT FALSE,
    hit_count INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_accessed TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_cache_tenant_id ON semantic_cache(tenant_id);
CREATE INDEX idx_cache_prompt_hash ON semantic_cache(prompt_hash);