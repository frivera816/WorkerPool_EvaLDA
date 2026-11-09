package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DBHost     string
	DBPort     int
	DBDatabase string
	DBUsername string
	DBPassword string

	WorkerCount int
	ClientLimit int
	BatchSize   int

	// DryRun controla la persistencia. true (valor seguro por defecto) solo
	// evalúa; false permite guardar el lote piloto de cinco clientes.
	DryRun bool

	QueryTimeout time.Duration
}

const (
	defaultDBHost       = "127.0.0.1"
	defaultDBPort       = 3306
	defaultWorkerCount  = 5
	defaultClientLimit  = 500
	defaultBatchSize    = 100
	defaultDryRun       = true
	defaultQueryTimeout = 30 * time.Second
)

// Load reads simple KEY=VALUE entries from path without adding a dotenv
// dependency. Process environment variables take precedence over the file.
func Load(path string) (Config, error) {
	if err := loadEnvFile(path); err != nil {
		return Config{}, err
	}

	cfg := Config{
		DBHost:       getString("DB_HOST", defaultDBHost),
		DBDatabase:   strings.TrimSpace(os.Getenv("DB_DATABASE")),
		DBUsername:   strings.TrimSpace(os.Getenv("DB_USERNAME")),
		DBPassword:   os.Getenv("DB_PASSWORD"),
		WorkerCount:  defaultWorkerCount,
		ClientLimit:  defaultClientLimit,
		BatchSize:    defaultBatchSize,
		DryRun:       defaultDryRun,
		QueryTimeout: defaultQueryTimeout,
	}

	port, err := getInt("DB_PORT", defaultDBPort)
	if err != nil {
		return Config{}, err
	}
	cfg.DBPort = port

	if cfg.WorkerCount, err = getInt("WORKER_COUNT", defaultWorkerCount); err != nil {
		return Config{}, err
	}
	if cfg.ClientLimit, err = getInt("CLIENT_LIMIT", defaultClientLimit); err != nil {
		return Config{}, err
	}
	if cfg.BatchSize, err = getInt("BATCH_SIZE", defaultBatchSize); err != nil {
		return Config{}, err
	}
	if cfg.DryRun, err = getBool("DRY_RUN", defaultDryRun); err != nil {
		return Config{}, err
	}

	timeoutValue := getString("DB_QUERY_TIMEOUT", defaultQueryTimeout.String())
	cfg.QueryTimeout, err = time.ParseDuration(timeoutValue)
	if err != nil {
		return Config{}, fmt.Errorf("DB_QUERY_TIMEOUT no es una duración válida: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.DBDatabase == "" {
		return errors.New("DB_DATABASE está vacío; complete el archivo .env")
	}
	if c.DBUsername == "" {
		return errors.New("DB_USERNAME está vacío; complete el archivo .env")
	}
	if c.DBHost == "" {
		return errors.New("DB_HOST no puede estar vacío")
	}
	if c.DBPort < 1 || c.DBPort > 65535 {
		return errors.New("DB_PORT debe estar entre 1 y 65535")
	}
	if c.WorkerCount < 1 {
		return errors.New("WORKER_COUNT debe ser mayor que cero")
	}
	if c.ClientLimit < 0 {
		return errors.New("CLIENT_LIMIT no puede ser negativo")
	}
	if c.BatchSize < 1 {
		return errors.New("BATCH_SIZE debe ser mayor que cero")
	}
	if c.QueryTimeout <= 0 {
		return errors.New("DB_QUERY_TIMEOUT debe ser mayor que cero")
	}
	return nil
}

func loadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("no se pudo abrir %s: %w", path, err)
	}
	defer file.Close()

	values, err := parseEnv(file)
	if err != nil {
		return fmt.Errorf("archivo %s inválido: %w", path, err)
	}
	for key, value := range values {
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("no se pudo cargar la variable %s: %w", key, err)
		}
	}
	return nil
}

func parseEnv(file *os.File) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("línea %d sin '='", lineNumber)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("línea %d sin nombre de variable", lineNumber)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func getString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getInt(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s debe ser un entero: %w", key, err)
	}
	return value, nil
}

func getBool(key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s debe ser true o false: %w", key, err)
	}
	return value, nil
}
