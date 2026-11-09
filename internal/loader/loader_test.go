package loader

import (
	"testing"

	"worker_pool_eva/internal/database"
)

func TestPlaceholders(t *testing.T) {
	if got := placeholders(1); got != "?" {
		t.Fatalf("placeholders(1) = %q", got)
	}
	if got := placeholders(4); got != "?,?,?,?" {
		t.Fatalf("placeholders(4) = %q", got)
	}
}

func TestPaymentJoinSuffix(t *testing.T) {
	if got := hpJoinSuffix(database.TablePaymentsHP); got != "_hp" {
		t.Fatalf("sufijo HP = %q", got)
	}
	if got := hpJoinSuffix(database.TablePayments); got != "" {
		t.Fatalf("sufijo normal = %q", got)
	}
}
