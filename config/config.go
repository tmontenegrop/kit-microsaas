package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port        string
	DBPath      string
	Env         string
	StoragePath string
	AppURL      string

	FlowAPIKey    string
	FlowSecretKey string
	FlowBaseURL   string

	AllowedOrigins []string
}

func Load() (Config, error) {
	port := os.Getenv("PORT")
	if port == "" {
		port = ":8080"
	} else if port[0] != ':' && !strings.Contains(port, ":") {
		port = ":" + port
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./data/app.db"
	}

	env := os.Getenv("ENV")
	if env == "" {
		env = "development"
	}

	storagePath := os.Getenv("STORAGE_PATH")
	if storagePath == "" {
		storagePath = "./storage"
	}

	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		appURL = "http://localhost:8080"
	}

	flowAPIKey := os.Getenv("FLOW_API_KEY")
	flowSecretKey := os.Getenv("FLOW_SECRET_KEY")
	flowBaseURL := os.Getenv("FLOW_BASE_URL")
	if flowBaseURL == "" {
		flowBaseURL = "https://sandbox.flow.cl/api"
	}

	var allowedOrigins []string
	if origins := os.Getenv("ALLOWED_ORIGINS"); origins != "" {
		allowedOrigins = strings.Split(origins, ",")
	}

	return Config{
		Port:         port,
		DBPath:       dbPath,
		Env:          env,
		StoragePath:  storagePath,
		AppURL:       appURL,
		FlowAPIKey:   flowAPIKey,
		FlowSecretKey: flowSecretKey,
		FlowBaseURL:  flowBaseURL,
		AllowedOrigins: allowedOrigins,
	}, nil
}

func (c Config) IsDevelopment() bool {
	return c.Env == "development"
}

func (c Config) PortInt() int {
	p := strings.TrimLeft(c.Port, ":")
	n, err := strconv.Atoi(p)
	if err != nil {
		return 8080
	}
	return n
}

func (c Config) IsProduction() bool {
	return c.Env == "production"
}
