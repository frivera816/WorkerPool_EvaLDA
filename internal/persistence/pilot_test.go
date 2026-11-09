package persistence

import (
	"context"
	"testing"

	"worker_pool_eva/internal/models"
)

func TestSaveEvaluationsRequiresAtLeastOne(t *testing.T) {
	_, err := SaveEvaluations(context.Background(), nil, []models.Evaluation{})
	if err == nil {
		t.Fatal("se esperaba error para un lote vacío")
	}
}

func TestSaveEvaluationsRejectsDuplicateClients(t *testing.T) {
	evaluations := make([]models.Evaluation, 3)
	for i := range evaluations {
		evaluations[i].ClientID = 1
	}
	_, err := SaveEvaluations(context.Background(), nil, evaluations)
	if err == nil {
		t.Fatal("se esperaba error para clientes duplicados")
	}
}
