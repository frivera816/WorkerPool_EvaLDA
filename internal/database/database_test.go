package database

import "testing"

func TestPlaceholders(t *testing.T) {
	if got := placeholders(0); got != "" {
		t.Fatalf("placeholders(0) = %q", got)
	}
	if got := placeholders(3); got != "?,?,?" {
		t.Fatalf("placeholders(3) = %q", got)
	}
}

func TestTableAvailability(t *testing.T) {
	tables := TableAvailability{present: map[string]bool{
		TableClients:          true,
		TableContractsMarkets: true,
	}}
	if tables.MissingCoreTables()[0] != TableEvaluations {
		t.Fatalf("faltantes = %v", tables.MissingCoreTables())
	}
	if tables.ContractsAvailable() {
		t.Fatal("los contratos no deberían estar completos")
	}

	tables.present[TableContractsHP] = true
	if !tables.ContractsAvailable() {
		t.Fatal("los contratos deberían estar completos")
	}
}
