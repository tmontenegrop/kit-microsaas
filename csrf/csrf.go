package csrf

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net/http"
)

const CookieName = "csrf_token"
const HeaderName = "X-CSRF-Token"
const FormField = "_csrf"

type contextKey string

const tokenKey contextKey = "csrf_token"

func GenerateToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func TokenFromCookie(r *http.Request) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func TokenFromContext(ctx context.Context) string {
	v := ctx.Value(tokenKey)
	if v == nil {
		return ""
	}
	return v.(string)
}

func setCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		HttpOnly: false,
		Secure:   secure,
		MaxAge:   86400,
	})
}

func Middleware(next http.Handler, secure bool, exemptPaths ...string) http.Handler {
	exempt := map[string]bool{}
	for _, p := range exemptPaths {
		exempt[p] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exempt[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
			token := TokenFromCookie(r)
			if token == "" {
				var err error
				token, err = GenerateToken()
				if err == nil {
					setCookie(w, token, secure)
				} else {
					slog.Error("csrf generate token", "error", err)
				}
			}
			if token != "" {
				ctx := context.WithValue(r.Context(), tokenKey, token)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
			return
		}

		cookieToken := TokenFromCookie(r)
		if cookieToken == "" {
			http.Error(w, "CSRF token missing", http.StatusForbidden)
			return
		}

		headerToken := r.Header.Get(HeaderName)
		formToken := r.FormValue(FormField)

		if subtle.ConstantTimeCompare([]byte(headerToken), []byte(cookieToken)) != 1 &&
			subtle.ConstantTimeCompare([]byte(formToken), []byte(cookieToken)) != 1 {
			http.Error(w, "CSRF token invalido", http.StatusForbidden)
			return
		}

		ctx := context.WithValue(r.Context(), tokenKey, cookieToken)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
