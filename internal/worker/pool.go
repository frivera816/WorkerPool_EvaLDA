package worker

import (
	"context"
	"errors"
	"sync"

	"worker_pool_eva/internal/models"
)

type Evaluator interface {
	Evaluate(models.ClientData) models.Evaluation
}

type Pool struct {
	evaluator Evaluator
	workers   int
}

type result struct {
	evaluation models.Evaluation
	err        error
}

func New(evaluator Evaluator, workers int) *Pool {
	if workers < 1 {
		workers = 1
	}
	return &Pool{evaluator: evaluator, workers: workers}
}

// Process distributes clients through channels and returns successful
// evaluations. Individual failures do not stop the remaining work.
func (p *Pool) Process(ctx context.Context, clients []models.ClientData) ([]models.Evaluation, int) {
	if len(clients) == 0 {
		return nil, 0
	}

	jobs := make(chan models.ClientData)
	results := make(chan result, len(clients))

	var waitGroup sync.WaitGroup
	waitGroup.Add(p.workers)
	for i := 0; i < p.workers; i++ {
		go func() {
			defer waitGroup.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case client, ok := <-jobs:
					if !ok {
						return
					}
					evaluation, err := p.evaluateSafely(client)
					results <- result{evaluation: evaluation, err: err}
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, client := range clients {
			select {
			case <-ctx.Done():
				return
			case jobs <- client:
			}
		}
	}()

	go func() {
		waitGroup.Wait()
		close(results)
	}()

	evaluations := make([]models.Evaluation, 0, len(clients))
	failures := 0
	for item := range results {
		if item.err != nil {
			failures++
			continue
		}
		evaluations = append(evaluations, item.evaluation)
	}
	return evaluations, failures
}

func (p *Pool) evaluateSafely(client models.ClientData) (evaluation models.Evaluation, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.New("fallo interno durante la evaluación en memoria")
		}
	}()
	return p.evaluator.Evaluate(client), nil
}
