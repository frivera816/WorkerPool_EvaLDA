package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "\ufeff# comentario\nDB_HOST='db.local'\nDB_PASSWORD=\"valor con espacios\"\nVACIA=\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	values, err := parseEnv(file)
	if err != nil {
		t.Fatal(err)
	}
	if values["DB_HOST"] != "db.local" {
		t.Fatalf("DB_HOST = %q", values["DB_HOST"])
	}
	if values["DB_PASSWORD"] != "valor con espacios" {
		t.Fatalf("DB_PASSWORD no conservó el valor entre comillas")
	}
	if value, ok := values["VACIA"]; !ok || value != "" {
		t.Fatalf("VACIA = %q, presente=%v", value, ok)
	}
}

func TestLoadDryRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("# sin variables\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("DB_DATABASE", "database")
	t.Setenv("DB_USERNAME", "user")
	t.Setenv("DRY_RUN", "")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DryRun {
		t.Fatal("DRY_RUN ausente debe activar el modo sin persistencia")
	}

	t.Setenv("DRY_RUN", "false")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DryRun {
		t.Fatal("DRY_RUN=false debe habilitar la persistencia")
	}

	t.Setenv("DRY_RUN", "valor-invalido")
	if _, err := Load(path); err == nil {
		t.Fatal("se esperaba error para DRY_RUN inválido")
	}
}

func TestValidate(t *testing.T) {
	valid := Config{
		DBHost:       "127.0.0.1",
		DBPort:       3306,
		DBDatabase:   "database",
		DBUsername:   "user",
		WorkerCount:  5,
		ClientLimit:  500,
		BatchSize:    100,
		QueryTimeout: 1,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("configuración válida rechazada: %v", err)
	}

	invalid := valid
	invalid.WorkerCount = 0
	if err := invalid.Validate(); err == nil {
		t.Fatal("se esperaba error para WORKER_COUNT=0")
	}
}
