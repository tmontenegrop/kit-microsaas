package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/tmontenegrop/kit-microsaas/cleanup"
	"github.com/tmontenegrop/kit-microsaas/config"
	"github.com/tmontenegrop/kit-microsaas/csrf"
	"github.com/tmontenegrop/kit-microsaas/db"
	"github.com/tmontenegrop/kit-microsaas/docgen"
	"github.com/tmontenegrop/kit-microsaas/middleware"
	"github.com/tmontenegrop/kit-microsaas/security"
	"github.com/tmontenegrop/kit-microsaas/template"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}

	if err := db.Open(cfg.DBPath); err != nil {
		slog.Error("db", "error", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	if err := db.Migrate("./db/migrations"); err != nil {
		slog.Error("migrate", "error", err)
		os.Exit(1)
	}

	go cleanup.Run(db.Conn, 1*time.Hour)

	db.Conn.Exec("INSERT OR IGNORE INTO tools (id, slug, name, description, price_clp) VALUES ('docgen', 'docgen', 'DocGen', 'Generador de documentos Word desde plantillas', 2990)")

	tmpl := template.New(cfg, "./views")

	mux := http.NewServeMux()

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		tmpl.Render(w, r, "index", template.TemplateData{
			Title: "Inicio",
		})
	})

	mux.HandleFunc("GET /tools/docgen", func(w http.ResponseWriter, r *http.Request) {
		tmpl.Render(w, r, "tools/docgen", template.TemplateData{
			Title:          "Generador de Documentos",
			IdempotencyKey: security.GenerateID(),
		})
	})

	doc := docgen.NewHandler(&cfg, tmpl)
	mux.HandleFunc("POST /tools/docgen", doc.Upload)
	mux.HandleFunc("GET /tools/docgen/{id}", doc.Show)
	mux.HandleFunc("GET /tools/docgen/{id}/template.xlsx", doc.DownloadTemplate)
	mux.HandleFunc("POST /tools/docgen/{id}/filename-markers", doc.ToggleFileNameMarker)
	mux.HandleFunc("POST /tools/docgen/{id}/data", doc.DataUpload)
	mux.HandleFunc("POST /tools/docgen/{id}/pay", doc.Pay)
	mux.HandleFunc("GET /status/{token}", doc.Status)
	mux.HandleFunc("GET /download/{token}", doc.Download)
	mux.HandleFunc("POST /webhook/flow", doc.Webhook)

	corsCfg := middleware.DefaultCORSConfig()
	if len(cfg.AllowedOrigins) > 0 {
		corsCfg.AllowedOrigins = cfg.AllowedOrigins
	}

	handler := middleware.Chain(
		middleware.Recovery,
		middleware.HTTPSRedirect(cfg.IsProduction()),
		middleware.SecurityHeaders,
		middleware.HSTS,
		middleware.CORS(corsCfg),
		middleware.RequestLogger,
	)(csrf.Middleware(mux, cfg.IsProduction(), "/webhook/flow"))

	slog.Info("servidor iniciado", "port", cfg.Port, "env", cfg.Env)
	if err := http.ListenAndServe(cfg.Port, handler); err != nil {
		slog.Error("servidor", "error", err)
		os.Exit(1)
	}
}
