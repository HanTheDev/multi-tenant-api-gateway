package auth

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

const TenantContextKey contextKey = "tenant"

type Middleware struct {
	jwtSecret string
}

func NewMiddleware(jwtSecret string) *Middleware {
	return &Middleware{jwtSecret: jwtSecret}
}

func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Missing authorization header", http.StatusUnauthorized)
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			http.Error(w, "Invalid authorization header format", http.StatusUnauthorized)
			return
		}

		claims, err := ValidateToken(parts[1], m.jwtSecret)
		if err != nil {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), TenantContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetTenantFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(TenantContextKey).(*Claims)
	return claims, ok
}
