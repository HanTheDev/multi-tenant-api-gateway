package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/HanTheDev/multi-tenant-api-gateway/internal/db"
	"github.com/HanTheDev/multi-tenant-api-gateway/internal/models"
	"github.com/redis/go-redis/v9"
)

type SemanticCache struct {
	db                  *db.DB
	redis               *redis.Client
	embeddingService    string
	similarityThreshold float64
}

func NewSemanticCache(database *db.DB, redisURL, embeddingService string) (*SemanticCache, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(opt)

	return &SemanticCache{
		db:                  database,
		redis:               client,
		embeddingService:    embeddingService,
		similarityThreshold: 0.85, // 85% similarity threshold
	}, nil
}

func (sc *SemanticCache) hashPrompt(prompt string) string {
	hash := sha256.Sum256([]byte(prompt))
	return fmt.Sprintf("%x", hash)
}

func (sc *SemanticCache) GetCachedResponse(ctx context.Context, tenantID int, prompt string) (string, bool, error) {
	promptHash := sc.hashPrompt(prompt)

	// Try exact match first
	cached, err := sc.db.GetCachedResponse(ctx, tenantID, promptHash)
	if err == nil {
		return cached.Response, true, nil
	}

	// Try semantic search in Redis
	embedding, err := sc.getEmbedding(prompt)
	if err != nil {
		return "", false, err
	}

	// Store embedding as vector in Redis
	embeddingKey := fmt.Sprintf("embedding:tenant:%d:prompt:%s", tenantID, promptHash)
	embeddingJSON, _ := json.Marshal(embedding)
	sc.redis.Set(ctx, embeddingKey, embeddingJSON, 0)

	// Search for similar embeddings
	// For simplicity, we check all cached prompts (in production, use vector DB)
	// This is a simplified version - you'd want Redis Vector Search or similar

	return "", false, nil
}

func (sc *SemanticCache) StoreCachedResponse(ctx context.Context, tenantID int, prompt, response string) error {
	promptHash := sc.hashPrompt(prompt)

	cache := &models.SemanticCache{
		TenantID:        tenantID,
		PromptHash:      promptHash,
		Prompt:          prompt,
		Response:        response,
		EmbeddingStored: false,
	}

	err := sc.db.StoreCachedResponse(ctx, cache)
	if err != nil {
		return err
	}

	// Store embedding asynchronously
	go func() {
		embedding, err := sc.getEmbedding(prompt)
		if err != nil {
			return
		}

		embeddingKey := fmt.Sprintf("embedding:tenant:%d:prompt:%s", tenantID, promptHash)
		embeddingJSON, _ := json.Marshal(embedding)
		sc.redis.Set(context.Background(), embeddingKey, embeddingJSON, 0)
	}()

	return nil
}

func (sc *SemanticCache) getEmbedding(text string) ([]float64, error) {
	reqBody, _ := json.Marshal(map[string]string{"text": text})

	resp, err := http.Post(
		sc.embeddingService+"/embed",
		"application/json",
		bytes.NewBuffer(reqBody),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Embedding []float64 `json:"embedding"`
	}

	json.Unmarshal(body, &result)
	return result.Embedding, nil
}
