package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	UserID     uuid.UUID `json:"uid"`
	TenantID   uuid.UUID `json:"tid"`
	Role       string    `json:"role"`
	TenantName string    `json:"tname"`
	Scope      string    `json:"scope,omitempty"` // "ws" for WebSocket-only tokens
	jwt.RegisteredClaims
}

// Sign issues a signed JWT for the dashboard session.
func Sign(secret []byte, userID, tenantID uuid.UUID, role, tenantName string, ttl time.Duration) (string, error) {
	return sign(secret, userID, tenantID, role, tenantName, "", ttl)
}

// SignScoped issues a short-lived token restricted to a single scope (e.g. "ws").
func SignScoped(secret []byte, userID, tenantID uuid.UUID, role, tenantName, scope string, ttl time.Duration) (string, error) {
	return sign(secret, userID, tenantID, role, tenantName, scope, ttl)
}

func sign(secret []byte, userID, tenantID uuid.UUID, role, tenantName, scope string, ttl time.Duration) (string, error) {
	claims := Claims{
		UserID:     userID,
		TenantID:   tenantID,
		Role:       role,
		TenantName: tenantName,
		Scope:      scope,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "mafafd",
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(secret)
}

var ErrInvalidToken = errors.New("invalid token")

// Parse validates a token and returns its claims.
func Parse(secret []byte, tokenString string) (*Claims, error) {
	tok, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return secret, nil
	}, jwt.WithIssuer("mafafd"), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, err
	}
	claims, ok := tok.Claims.(*Claims)
	if !ok || !tok.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
