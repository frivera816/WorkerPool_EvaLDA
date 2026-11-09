package persistence

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"worker_pool_eva/internal/database"
	"worker_pool_eva/internal/evaluator"
	"worker_pool_eva/internal/models"
)

const (
	EvaluatorUser   = "worker_pool_eva"
	PilotComment    = "Evaluación automática - lote piloto Worker Pool EVA"
	MassComment     = "Evaluación automática - procesamiento masivo Worker Pool EVA"
	tableEvaluation = "scc_creditos_evaluacion_riesgo_cliente"
)

type SaveResult struct {
	EvaluationIDs []int64
	ClientIDs     []int64
}

// SaveEvaluations inserts all provided evaluations in a single transaction.
// Existing evaluations are never updated or deleted. Duplicate client IDs are
// rejected before any INSERT is executed.
func SaveEvaluations(ctx context.Context, db *database.DB, evaluations []models.Evaluation) (SaveResult, error) {
	if len(evaluations) < 1 {
		return SaveResult{}, fmt.Errorf("el lote debe contener al menos una evaluación; recibidas: %d", len(evaluations))
	}
	seen := make(map[int64]bool, len(evaluations))
	for _, evaluation := range evaluations {
		if evaluation.ClientID <= 0 {
			return SaveResult{}, fmt.Errorf("el lote contiene un id_cliente inválido")
		}
		if seen[evaluation.ClientID] {
			return SaveResult{}, fmt.Errorf("el lote contiene id_cliente duplicado: %d", evaluation.ClientID)
		}
		seen[evaluation.ClientID] = true
	}

	tx, err := db.BeginTx(ctx)
	if err != nil {
		return SaveResult{}, fmt.Errorf("no se pudo iniciar la transacción del lote: %w", err)
	}
	defer tx.Rollback()

	ids := make([]any, len(evaluations))
	for i, evaluation := range evaluations {
		ids[i] = evaluation.ClientID
	}
	duplicateQuery := `
SELECT id_cliente
FROM ` + tableEvaluation + `
WHERE id_cliente IN (` + placeholders(len(ids)) + `)
FOR UPDATE`

	rows, err := tx.QueryContext(ctx, duplicateQuery, ids...)
	if err != nil {
		return SaveResult{}, fmt.Errorf("no se pudo comprobar la existencia de evaluaciones previas: %w", err)
	}
	var duplicates []int64
	for rows.Next() {
		var clientID int64
		if err := rows.Scan(&clientID); err != nil {
			rows.Close()
			return SaveResult{}, fmt.Errorf("no se pudo leer una evaluación previa: %w", err)
		}
		duplicates = append(duplicates, clientID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return SaveResult{}, fmt.Errorf("no se pudo completar la comprobación de evaluaciones previas")
	}
	rows.Close()
	if len(duplicates) > 0 {
		return SaveResult{}, fmt.Errorf("persistencia cancelada: ya existen evaluaciones para los clientes %v", duplicates)
	}

	const insertQuery = `
INSERT INTO scc_creditos_evaluacion_riesgo_cliente (
    id_cliente, fecha_evaluacion,
    score_tipo_cliente, score_frecuencia, score_forma_pago, score_nacionalidad,
    score_anios_relacion, score_informacion, score_pep, score_apnfd,
    score_listas_noticias, score_procedencia_fondos, promedio_cliente,
    score_casa_matriz, score_gestor_credito, promedio_canales,
    score_zona_ubicacion, score_delitos_lda, score_paises_gafi,
    score_domicilio, score_fondos_negros, promedio_geografia,
    calificacion_final, nivel_riesgo, debida_diligencia,
    usuario_evaluador, comentarios_observaciones
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	evaluatedAt := time.Now().Format("2006-01-02 15:04:05")
	comment := MassComment
	result := SaveResult{
		EvaluationIDs: make([]int64, 0, len(evaluations)),
		ClientIDs:     make([]int64, 0, len(evaluations)),
	}
	for i, evaluation := range evaluations {
		scores := evaluation.Scores
		summary := evaluator.Summarize(scores)
		if i > 0 && i%500 == 0 {
			log.Printf("persistencia: %d/%d insertados", i, len(evaluations))
		}
		execResult, err := tx.ExecContext(
			ctx,
			insertQuery,
			evaluation.ClientID,
			evaluatedAt,
			scores.TipoCliente.Value,
			scores.Frecuencia.Value,
			scores.FormaPago.Value,
			scores.Nacionalidad.Value,
			scores.AniosRelacion.Value,
			scores.InformacionPerfil.Value,
			scores.PEP.Value,
			scores.APNFD.Value,
			scores.NoticiasNegativas.Value,
			scores.ProcedenciaFondos.Value,
			summary.AverageClient,
			scores.CasaMatriz.Value,
			scores.GestorCreditos.Value,
			summary.AverageChannels,
			scores.ZonaGeografica.Value,
			scores.PresenciaDelitos.Value,
			scores.PaisesGAFI.Value,
			scores.Domicilio.Value,
			scores.FondosTransferidos.Value,
			summary.AverageGeography,
			summary.FinalScore,
			summary.RiskLevel,
			summary.DueDiligence,
			EvaluatorUser,
			comment,
		)
		if err != nil {
			return SaveResult{}, fmt.Errorf("falló la inserción del cliente %d; se revirtió el lote completo: %w", evaluation.ClientID, err)
		}
		affected, err := execResult.RowsAffected()
		if err != nil || affected != 1 {
			return SaveResult{}, fmt.Errorf("la inserción del cliente %d no confirmó una fila; se revirtió el lote completo", evaluation.ClientID)
		}
		evaluationID, err := execResult.LastInsertId()
		if err != nil {
			return SaveResult{}, fmt.Errorf("no se pudo obtener el id de la evaluación insertada; se revirtió el lote completo")
		}
		result.EvaluationIDs = append(result.EvaluationIDs, evaluationID)
		result.ClientIDs = append(result.ClientIDs, evaluation.ClientID)
	}

	if err := tx.Commit(); err != nil {
		return SaveResult{}, fmt.Errorf("no se pudo confirmar la transacción; no se considera guardado el lote: %w", err)
	}
	return result, nil
}

// PilotSize se mantiene para compatibilidad con código existente.
const PilotSize = 5

func placeholders(count int) string {
	if count < 1 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("?")
	for i := 1; i < count; i++ {
		builder.WriteString(",?")
	}
	return builder.String()
}
