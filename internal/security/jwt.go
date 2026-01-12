package security

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWT validation errors.
var (
	// ErrInvalidToken indicates a token is malformed or fails validation.
	ErrInvalidToken = errors.New("invalid token")
	// ErrExpiredToken indicates a token has expired.
	ErrExpiredToken = errors.New("token expired")
)

// UserClaims defines JWT claims for end users.
type UserClaims struct {
	UserID   uint64 `json:"user_id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	jwt.RegisteredClaims
}

// AdminClaims defines JWT claims for administrators.
type AdminClaims struct {
	AdminID  uint64 `json:"admin_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// GenerateToken signs a user JWT with the configured expiry.
func GenerateToken(secret string, userID uint64, username, name, email string, expiry time.Duration) (string, error) {
	now := time.Now().UTC()
	claims := UserClaims{
		UserID:   userID,
		Username: username,
		Name:     name,
		Email:    email,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseToken validates a user JWT and returns its claims.
func ParseToken(secret string, tokenString string) (*UserClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &UserClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(secret), nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}
	claims, ok := token.Claims.(*UserClaims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// GenerateAdminToken signs an admin JWT with the configured expiry.
func GenerateAdminToken(secret string, adminID uint64, username string, expiry time.Duration) (string, error) {
	now := time.Now().UTC()
	claims := AdminClaims{
		AdminID:  adminID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseAdminToken validates an admin JWT and returns its claims.
func ParseAdminToken(secret string, tokenString string) (*AdminClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &AdminClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(secret), nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}
	claims, ok := token.Claims.(*AdminClaims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
