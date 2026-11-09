package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"time"

	"worker_pool_eva/internal/config"
	"worker_pool_eva/internal/database"
	"worker_pool_eva/internal/evaluator"
	"worker_pool_eva/internal/loader"
	"worker_pool_eva/internal/models"
	"worker_pool_eva/internal/persistence"
	"worker_pool_eva/internal/worker"
)

type metrics struct {
	selected     int
	evaluated    int
	errors       int
	automatic    int64
	defaults     int64
	loadTime     time.Duration
	evaluation   time.Duration
	queryCount   int64
	memoryBefore runtime.MemStats
	memoryAfter  runtime.MemStats
}

func main() {
	if err := run(); err != nil {
		log.Printf("error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	envPath := flag.String("env", ".env", "ruta del archivo de configuración")
	flag.Parse()

	cfg, err := config.Load(*envPath)
	if err != nil {
		return err
	}
	// Persistencia controlada por DRY_RUN:
	//   DRY_RUN=true  -> solo evalúa, no escribe.
	//   DRY_RUN=false -> evalúa y persiste TODOS los clientes seleccionados.
	persistEvaluations := !cfg.DryRun

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	openCtx, cancel := context.WithTimeout(ctx, cfg.QueryTimeout)
	db, err := database.Open(openCtx, cfg)
	cancel()
	if err != nil {
		return err
	}
	defer db.Close()

	limitLabel := fmt.Sprintf("%d", cfg.ClientLimit)
	if cfg.ClientLimit == 0 {
		limitLabel = "todos (sin límite)"
	}
	fmt.Printf("Configuración: workers=%d, límite_clientes=%s, tamaño_lote=%d, timeout_consulta=%s, dry_run=%t\n",
		cfg.WorkerCount, limitLabel, cfg.BatchSize, cfg.QueryTimeout, cfg.DryRun)

	totalStart := time.Now()
	var currentMetrics metrics
	runtime.ReadMemStats(&currentMetrics.memoryBefore)

	loadStart := time.Now()
	startupCtx, cancel := context.WithTimeout(ctx, cfg.QueryTimeout)
	tables, err := db.DetectTables(startupCtx)
	cancel()
	if err != nil {
		return err
	}
	if missing := tables.MissingCoreTables(); len(missing) > 0 {
		return fmt.Errorf("faltan tablas obligatorias: %v", missing)
	}
	if !tables.ContractsAvailable() {
		fmt.Println("Aviso: falta una fuente obligatoria de contratos; A1, A2, A3 y A5 usarán valores predeterminados.")
	}

	selectCtx, cancel := context.WithTimeout(ctx, cfg.QueryTimeout)
	ids, err := db.SelectClientsWithoutEvaluation(selectCtx, cfg.ClientLimit)
	cancel()
	if err != nil {
		return err
	}
	currentMetrics.loadTime += time.Since(loadStart)
	currentMetrics.selected = len(ids)

	fmt.Printf("Clientes sin evaluación seleccionados: %d\n", len(ids))
	if len(ids) == 0 {
		currentMetrics.queryCount = db.QueryCount()
		runtime.ReadMemStats(&currentMetrics.memoryAfter)
		printMetrics(currentMetrics, cfg.WorkerCount, time.Since(totalStart), false)
		return nil
	}

	engine := evaluator.New(time.Now(), tables.ContractsAvailable())
	pool := worker.New(engine, cfg.WorkerCount)
	clientLoader := loader.New(db, tables)
	var pilotEvaluations []models.Evaluation

	for batchStart := 0; batchStart < len(ids); batchStart += cfg.BatchSize {
		if err := ctx.Err(); err != nil {
			break
		}
		batchEnd := batchStart + cfg.BatchSize
		if batchEnd > len(ids) {
			batchEnd = len(ids)
		}
		batch := ids[batchStart:batchEnd]

		loadCtx, cancel := context.WithTimeout(ctx, cfg.QueryTimeout)
		loadStart := time.Now()
		clients, loadErr := clientLoader.LoadBatch(loadCtx, batch)
		cancel()
		currentMetrics.loadTime += time.Since(loadStart)
		if loadErr != nil {
			log.Printf("lote %d: error de carga: %v", batchStart/cfg.BatchSize+1, loadErr)
			currentMetrics.errors += len(batch)
			continue
		}
		currentMetrics.errors += len(batch) - len(clients)

		evaluationStart := time.Now()
		evaluations, failures := pool.Process(ctx, clients)
		currentMetrics.evaluation += time.Since(evaluationStart)
		currentMetrics.errors += failures

		for _, evaluation := range evaluations {
			currentMetrics.evaluated++
			currentMetrics.automatic += int64(evaluation.AutomaticCount)
			currentMetrics.defaults += int64(evaluation.DefaultCount)
			if persistEvaluations {
				pilotEvaluations = append(pilotEvaluations, evaluation)
			}
		}
		// En modo benchmark las evaluaciones del lote se descartan.
	}

	// Si se interrumpió el proceso, los clientes no evaluados cuentan como error.
	if unaccounted := currentMetrics.selected - currentMetrics.evaluated - currentMetrics.errors; unaccounted > 0 {
		currentMetrics.errors += unaccounted
	}

	if persistEvaluations {
		if currentMetrics.errors != 0 || len(pilotEvaluations) < 1 {
			return fmt.Errorf("no se persiste el lote: evaluaciones=%d, errores=%d", len(pilotEvaluations), currentMetrics.errors)
		}
		sort.Slice(pilotEvaluations, func(i, j int) bool {
			return pilotEvaluations[i].ClientID < pilotEvaluations[j].ClientID
		})
		printPilotEvaluations(pilotEvaluations)

		saveCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		saveResult, saveErr := persistence.SaveEvaluations(saveCtx, db, pilotEvaluations)
		cancel()
		if saveErr != nil {
			log.Printf("error de persistencia: %v", saveErr)
			return saveErr
		}
		printPilotReport(pilotEvaluations, saveResult, currentMetrics)
	}

	currentMetrics.queryCount = db.QueryCount()
	runtime.ReadMemStats(&currentMetrics.memoryAfter)
	totalDuration := time.Since(totalStart)
	printMetrics(currentMetrics, cfg.WorkerCount, totalDuration, persistEvaluations)
	return nil
}

func printPilotEvaluations(evaluations []models.Evaluation) {
	fmt.Println()
	fmt.Println("=== Evaluaciones generadas antes de persistir ===")
	for _, evaluation := range evaluations {
		allScores := evaluation.Scores.All()
		summary := evaluator.Summarize(evaluation.Scores)
		fmt.Printf("\nCliente ID: %d\n", evaluation.ClientID)
		fmt.Println("Tabla A - Riesgo del cliente o contraparte")
		for _, score := range allScores[:10] {
			printScore(score)
		}
		fmt.Printf("Promedio Tabla A: %.2f\n", summary.AverageClient)
		fmt.Println("Tabla B - Riesgo de la zona geográfica")
		for _, score := range allScores[10:15] {
			printScore(score)
		}
		fmt.Printf("Promedio Tabla B: %.2f\n", summary.AverageGeography)
		fmt.Println("Tabla C - Riesgo de canales de distribución")
		for _, score := range allScores[15:17] {
			printScore(score)
		}
		fmt.Printf("Promedio Tabla C: %.2f\n", summary.AverageChannels)
		fmt.Printf("Calificación final: %.2f\n", summary.FinalScore)
		fmt.Printf("Nivel de riesgo: %s\n", summary.RiskLevel)
		fmt.Printf("Debida diligencia: %s\n", summary.DueDiligence)
	}
}

func printScore(score models.Score) {
	source := "predeterminada"
	if score.Source == models.SourceAutomatic {
		source = "automática"
	}
	fmt.Printf("%s %s: %.4f | %s | %s\n", score.Code, score.Name, score.Value, source, score.Reason)
}

func printPilotReport(evaluations []models.Evaluation, result persistence.SaveResult, m metrics) {
	fmt.Println()
	fmt.Println("=== Persistencia del lote piloto ===")
	fmt.Printf("IDs de clientes procesados:          %v\n", result.ClientIDs)
	fmt.Printf("IDs de evaluaciones insertadas:      %v\n", result.EvaluationIDs)
	fmt.Printf("Evaluaciones generadas:              %d\n", len(evaluations))
	fmt.Printf("Registros guardados:                 %d\n", len(result.EvaluationIDs))
	fmt.Printf("Puntuaciones automáticas:            %d\n", m.automatic)
	fmt.Printf("Puntuaciones predeterminadas:        %d\n", m.defaults)
	fmt.Printf("Errores:                             %d\n", m.errors)
	fmt.Println("Evaluaciones anteriores modificadas: 0")
	fmt.Println("Campo de revisión:                   no existe en el esquema; no se asignó estado de revisión.")
	fmt.Println("Solo se ejecutaron 5 INSERT en una transacción; no hubo UPDATE ni DELETE.")
}

func printMetrics(m metrics, workers int, total time.Duration, persisted bool) {
	seconds := total.Seconds()
	perSecond := 0.0
	if seconds > 0 {
		perSecond = float64(m.evaluated) / seconds
	}

	fmt.Println()
	if persisted {
		fmt.Println("=== Métricas del lote piloto con persistencia ===")
	} else {
		fmt.Println("=== Resultados del benchmark (sin persistencia) ===")
	}
	fmt.Printf("Clientes seleccionados:              %d\n", m.selected)
	fmt.Printf("Clientes evaluados correctamente:    %d\n", m.evaluated)
	fmt.Printf("Errores:                             %d\n", m.errors)
	fmt.Printf("Puntuaciones automáticas:            %d\n", m.automatic)
	fmt.Printf("Puntuaciones predeterminadas:        %d\n", m.defaults)
	fmt.Printf("Consultas ejecutadas:                %d\n", m.queryCount)
	fmt.Printf("Tiempo de carga de datos:            %s\n", m.loadTime.Round(time.Millisecond))
	fmt.Printf("Tiempo de evaluación:                %s\n", m.evaluation.Round(time.Millisecond))
	fmt.Printf("Tiempo total:                        %s\n", total.Round(time.Millisecond))
	fmt.Printf("Clientes procesados por segundo:     %.2f\n", perSecond)
	fmt.Printf("Workers utilizados:                  %d\n", workers)
	fmt.Printf("Heap utilizado:                      %s\n", formatBytes(m.memoryAfter.HeapAlloc))
	fmt.Printf("Memoria del runtime:                 %s\n", formatBytes(m.memoryAfter.Sys))
	if m.memoryAfter.TotalAlloc >= m.memoryBefore.TotalAlloc {
		fmt.Printf("Memoria total asignada en el lote:   %s\n", formatBytes(m.memoryAfter.TotalAlloc-m.memoryBefore.TotalAlloc))
	}
	fmt.Printf("Recolecciones de basura:             %d\n", m.memoryAfter.NumGC-m.memoryBefore.NumGC)
	if persisted {
		fmt.Println("Persistencia confirmada para el lote piloto de cinco clientes.")
	} else {
		fmt.Println("Resultados descartados: no se ejecutaron escrituras en MariaDB.")
	}
}

func formatBytes(bytes uint64) string {
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
	)
	switch {
	case bytes >= gib:
		return fmt.Sprintf("%.2f GiB", float64(bytes)/gib)
	case bytes >= mib:
		return fmt.Sprintf("%.2f MiB", float64(bytes)/mib)
	case bytes >= kib:
		return fmt.Sprintf("%.2f KiB", float64(bytes)/kib)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
