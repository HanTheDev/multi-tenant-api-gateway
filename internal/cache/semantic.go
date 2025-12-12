package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
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

	// 1. Try exact match first (fastest)
	cached, err := sc.db.GetCachedResponse(ctx, tenantID, promptHash)
	if err == nil {
		return cached.Response, true, nil
	}

	// 2. Try semantic search
	queryEmbedding, err := sc.getEmbedding(prompt)
	if err != nil {
		return "", false, fmt.Errorf("failed to get embedding: %w", err)
	}

	// Get all cached prompts for this tenant from Redis
	pattern := fmt.Sprintf("embedding:tenant:%d:*", tenantID)
	keys, err := sc.redis.Keys(ctx, pattern).Result()
	if err != nil {
		return "", false, nil
	}

	// Find most similar cached prompt
	var bestMatch string
	bestSimilarity := 0.0

	for _, key := range keys {
		cachedEmbeddingJSON, err := sc.redis.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var cachedEmbedding []float64
		if err := json.Unmarshal([]byte(cachedEmbeddingJSON), &cachedEmbedding); err != nil {
			continue
		}

		similarity := cosineSimilarity(queryEmbedding, cachedEmbedding)

		if similarity > bestSimilarity && similarity >= sc.similarityThreshold {
			bestSimilarity = similarity
			// Extract prompt hash from key: "embedding:tenant:1:prompt:abc123"
			bestMatch = key[len(fmt.Sprintf("embedding:tenant:%d:prompt:", tenantID)):]
		}
	}

	// If we found a similar prompt, get its response
	if bestMatch != "" {
		cached, err := sc.db.GetCachedResponse(ctx, tenantID, bestMatch)
		if err == nil {
			return cached.Response, true, nil
		}
	}

	return "", false, nil
}

func (sc *SemanticCache) StoreCachedResponse(ctx context.Context, tenantID int, prompt, response string) error {
	promptHash := sc.hashPrompt(prompt)

	// Store in PostgreSQL
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
		bgCtx := context.Background()

		embedding, err := sc.getEmbedding(prompt)
		if err != nil {
			return
		}

		embeddingKey := fmt.Sprintf("embedding:tenant:%d:prompt:%s", tenantID, promptHash)
		embeddingJSON, _ := json.Marshal(embedding)

		// Store with 7-day expiration
		sc.redis.Set(bgCtx, embeddingKey, embeddingJSON, 7*24*60*60)
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

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return result.Embedding, nil
}

// Calculate cosine similarity between two vectors
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
