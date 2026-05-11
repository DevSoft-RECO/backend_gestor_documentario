package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Port               string
	DBDriver           string
	DBURL              string
	MotherAPIURL       string
	FrontendURL        string
	AllowedOrigins     string
	OAuthPublicKeyPath string
}

var Envs *Config

func LoadConfig() {
	// Simple .env loader since we don't have godotenv yet
	loadEnvFile(".env")

	Envs = &Config{
		Port:               getEnv("PORT", "8001"),
		DBDriver:           strings.ToLower(getEnv("DB_DRIVER", "")),
		DBURL:              getEnv("DATABASE_URL", ""),
		MotherAPIURL:       getEnv("APP_MADRE_URL", "http://localhost:8000"),
		FrontendURL:        getEnv("APP_URL_FRONTEND", "http://localhost:5190"),
		AllowedOrigins:     getEnv("ALLOWED_ORIGINS", "*"),
		OAuthPublicKeyPath: getEnv("OAUTH_PUBLIC_KEY_PATH", "./keys/oauth-public.key"),
	}


	if Envs.DBURL == "" {
		fmt.Println("Warning: DATABASE_URL not set in .env")
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func loadEnvFile(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// Remove quotes if present
			value = strings.Trim(value, `"'`)
			os.Setenv(key, value)
		}
	}
}

func (c *Config) GetMySQLDSN() string {
	// Convert FastAPI/SQLAlchemy URL to Go MySQL DSN if necessary
	// SQLAlchemy: mysql+pymysql://root:@127.0.0.1:3306/creditos
	// Go DSN: root:@tcp(127.0.0.1:3306)/creditos?charset=utf8mb4&parseTime=True&Loc=Local

	dsn := c.DBURL

	// Handle mysql://user:pass@host:3306/db
	// (GORM's mysql driver expects the go-sql-driver/mysql DSN format.)
	if strings.HasPrefix(dsn, "mysql://") {
		trimmed := strings.TrimPrefix(dsn, "mysql://")
		parts := strings.Split(trimmed, "@")
		if len(parts) == 2 {
			creds := parts[0]
			hostDB := parts[1]
			hostDBParts := strings.SplitN(hostDB, "/", 2)
			if len(hostDBParts) == 2 {
				host := hostDBParts[0]
				dbWithParams := hostDBParts[1]
				return fmt.Sprintf("%s@tcp(%s)/%s", creds, host, dbWithParams)
			}
		}
	}

	if strings.Contains(dsn, "mysql+pymysql://") {
		dsn = strings.Replace(dsn, "mysql+pymysql://", "", 1)
		parts := strings.Split(dsn, "@")
		if len(parts) == 2 {
			creds := parts[0]
			hostDB := parts[1]
			hostDBParts := strings.Split(hostDB, "/")
			if len(hostDBParts) == 2 {
				host := hostDBParts[0]
				db := hostDBParts[1]
				return fmt.Sprintf("%s@tcp(%s)/%s?charset=utf8mb4&parseTime=True&loc=Local", creds, host, db)
			}
		}
	}
	return dsn
}

func (c *Config) GetPostgresDSN() string {
	// GORM PostgreSQL driver supports standard postgres:// URLs
	// We just ensure it's not the SQLAlchemy style if mistakenly used
	dsn := c.DBURL
	if strings.HasPrefix(dsn, "postgres+psycopg2://") {
		return strings.Replace(dsn, "postgres+psycopg2://", "postgres://", 1)
	}
	return dsn
}
