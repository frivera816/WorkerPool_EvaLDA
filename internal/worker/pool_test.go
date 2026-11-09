package worker

import (
	"context"
	"sync/atomic"
	"testing"

	"worker_pool_eva/internal/models"
)

type countingEvaluator struct {
	calls atomic.Int64
}

func (e *countingEvaluator) Evaluate(client models.ClientData) models.Evaluation {
	e.calls.Add(1)
	return models.Evaluation{ClientID: client.ID}
}

type panicEvaluator struct{}

func (panicEvaluator) Evaluate(client models.ClientData) models.Evaluation {
	if client.ID == 13 {
		panic("prueba")
	}
	return models.Evaluation{ClientID: client.ID}
}

func TestPoolProcessesAllClients(t *testing.T) {
	evaluator := &countingEvaluator{}
	pool := New(evaluator, 5)
	clients := make([]models.ClientData, 500)
	for i := range clients {
		clients[i].ID = int64(i + 1)
	}

	evaluations, failures := pool.Process(context.Background(), clients)
	if failures != 0 {
		t.Fatalf("fallos = %d", failures)
	}
	if len(evaluations) != len(clients) {
		t.Fatalf("evaluaciones = %d, esperadas = %d", len(evaluations), len(clients))
	}
	if evaluator.calls.Load() != int64(len(clients)) {
		t.Fatalf("llamadas = %d", evaluator.calls.Load())
	}
	seen := make(map[int64]bool, len(evaluations))
	for _, evaluation := range evaluations {
		seen[evaluation.ClientID] = true
	}
	for _, client := range clients {
		if !seen[client.ID] {
			t.Fatalf("cliente %d no fue evaluado", client.ID)
		}
	}
}

func TestPoolContinuesAfterIndividualPanic(t *testing.T) {
	pool := New(panicEvaluator{}, 3)
	clients := []models.ClientData{{ID: 1}, {ID: 13}, {ID: 20}}

	evaluations, failures := pool.Process(context.Background(), clients)
	if failures != 1 {
		t.Fatalf("fallos = %d, esperado = 1", failures)
	}
	if len(evaluations) != 2 {
		t.Fatalf("evaluaciones = %d, esperadas = 2", len(evaluations))
	}
}
