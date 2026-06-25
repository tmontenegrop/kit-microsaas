package middleware

import (
	"log/slog"
	"net/http"
)

func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered",
					"method", r.Method,
					"path", r.URL.Path,
					"error", rec,
				)
				if r.Header.Get("HX-Request") == "true" {
					http.Error(w, "Error interno del servidor", http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`<!DOCTYPE html><html lang="es"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Error</title><script src="https://cdn.tailwindcss.com"></script></head><body class="bg-gray-50 min-h-screen flex items-center justify-center"><div class="text-center max-w-md mx-auto px-4"><h1 class="text-6xl font-bold text-gray-300 mb-4">500</h1><p class="text-xl text-gray-600 mb-8">Error interno del servidor</p><p class="text-gray-500 mb-8">Ocurrió un error inesperado. Intenta nuevamente más tarde.</p><a href="/" class="inline-flex items-center px-4 py-2 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-blue-600 hover:bg-blue-700">Volver al inicio</a></div></body></html>`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func Chain(mws ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		for i := len(mws) - 1; i >= 0; i-- {
			next = mws[i](next)
		}
		return next
	}
}
