package utils

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt"
)

type JWTToken struct {
	config *Config
}

func NewJWTToken(config *Config) *JWTToken {
	return &JWTToken{config: config}
}

type jwtClaim struct {
	jwt.StandardClaims
	UserID   int64  `json:"user_id"`
	Role     string `json:"user_role"`
	Verified bool   `json:"user_verified"`
	Email    string `json:"email"`
	UserTag  string `json:"user_tag"`
	Exp      int64  `json:"exp"`
}

type TokenObject struct {
	UserID   int64  `json:"user_id"`
	Role     string `json:"user_role"`
	Verified bool   `json:"user_verified"`
	UserTag  string `json:"user_tag"`
	Email    string `json:"email"`
}

func (j *JWTToken) CreateToken(user TokenObject) (string, error) {
	claims := jwtClaim{
		UserID:   user.UserID,
		Role:     user.Role,
		Verified: user.Verified,
		Email:    user.Email,
		UserTag:  user.UserTag,
		Exp:      time.Now().Add(time.Hour * 24 * 30).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString([]byte(j.config.SigningKey))
	if err != nil {
		return "", err
	}

	// TODO: Add Cache Layer Validation for Password Changes etc.
	// https://stackoverflow.com/questions/21978658/invalidating-json-web-tokens
	// e.g security.CacheInstance.Insert(string(rune(user.UserID)))

	return string(tokenString), nil
}

func (j *JWTToken) VerifyToken(tokenString string) (TokenObject, error) {
	token, err := jwt.ParseWithClaims(tokenString, &jwtClaim{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("invalid authentication token, format error")
		}
		return []byte(j.config.SigningKey), nil
	})

	if err != nil {
		return TokenObject{}, fmt.Errorf("invalid authentication token, %v", err.Error())
	}

	claims, ok := token.Claims.(*jwtClaim)
	if !ok {
		return TokenObject{}, fmt.Errorf("invalid authentication token, token is not OK")
	}

	if claims.Exp < time.Now().Unix() {
		return TokenObject{}, fmt.Errorf("token is expired")
	}

	return TokenObject{
		UserID:   claims.UserID,
		Role:     claims.Role,
		Verified: claims.Verified,
		UserTag:  claims.UserTag,
		Email:    claims.Email,
	}, nil
}
