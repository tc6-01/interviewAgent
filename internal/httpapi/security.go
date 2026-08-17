package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const subjectCookieName = "interview_subject"

type SecurityConfig struct {
	Mode           string
	JWTSecret      string
	AllowedOrigins []string
	CookieSecure   bool
}

type subjectContextKey struct{}

func secureHandler(config SecurityConfig, next http.Handler) http.Handler {
	if strings.TrimSpace(config.Mode) == "" {
		config.Mode = "anonymous"
	}
	allowedOrigins := make(map[string]struct{}, len(config.AllowedOrigins))
	for _, origin := range config.AllowedOrigins {
		allowedOrigins[strings.TrimSpace(origin)] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'")

		if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")

		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" && !sameOrigin(origin, r) {
			if config.Mode != "jwt" {
				writeAPIError(w, http.StatusForbidden, "cross_origin_anonymous_forbidden", "匿名模式仅支持同源访问", nil)
				return
			}
			if _, ok := allowedOrigins[origin]; !ok {
				writeAPIError(w, http.StatusForbidden, "cors_origin_forbidden", "请求来源不在 CORS 白名单", nil)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, Last-Event-ID")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		subjectID, err := authenticateSubject(config, w, r)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "无法验证当前主体", nil)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), subjectContextKey{}, subjectID)))
	})
}

func authenticateSubject(config SecurityConfig, w http.ResponseWriter, r *http.Request) (string, error) {
	switch config.Mode {
	case "anonymous":
		if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
			return "", errors.New("bearer authentication is disabled")
		}
		if cookie, err := r.Cookie(subjectCookieName); err == nil && validAnonymousSubject(cookie.Value) {
			return cookie.Value, nil
		}
		subjectID := "anon_" + uuid.NewString()
		http.SetCookie(w, &http.Cookie{
			Name: subjectCookieName, Value: subjectID, Path: "/", MaxAge: int((30 * 24 * time.Hour).Seconds()),
			HttpOnly: true, Secure: config.CookieSecure, SameSite: http.SameSiteLaxMode,
		})
		return subjectID, nil
	case "jwt":
		return jwtSubject(r.Header.Get("Authorization"), config.JWTSecret)
	default:
		return "", errors.New("unsupported authentication mode")
	}
}

func validAnonymousSubject(value string) bool {
	if !strings.HasPrefix(value, "anon_") {
		return false
	}
	_, err := uuid.Parse(strings.TrimPrefix(value, "anon_"))
	return err == nil
}

func jwtSubject(authorization, secret string) (string, error) {
	parts := strings.Fields(authorization)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(secret) < 32 {
		return "", errors.New("invalid bearer authentication")
	}
	claims := jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(parts[1], &claims, func(token *jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid || strings.TrimSpace(claims.Subject) == "" {
		return "", errors.New("invalid bearer token")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(claims.Subject))
	return "jwt_" + hex.EncodeToString(mac.Sum(nil)), nil
}

func sameOrigin(origin string, r *http.Request) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return strings.EqualFold(parsed.Scheme, scheme) && strings.EqualFold(parsed.Host, r.Host)
}

func subjectFromRequest(r *http.Request) (string, error) {
	subjectID, ok := r.Context().Value(subjectContextKey{}).(string)
	if !ok || strings.TrimSpace(subjectID) == "" {
		return "", errors.New("subject is not authenticated")
	}
	return subjectID, nil
}
