package evaluator

import (
	"math"

	"worker_pool_eva/internal/models"
)

// Summarize applies the same averages and formula used by evaluacionRiesgoLDA.php.
func Summarize(scores models.Scores) models.RiskSummary {
	clientValues := []models.Score{
		scores.TipoCliente,
		scores.Frecuencia,
		scores.FormaPago,
		scores.Nacionalidad,
		scores.AniosRelacion,
		scores.InformacionPerfil,
		scores.PEP,
		scores.APNFD,
		scores.NoticiasNegativas,
		scores.ProcedenciaFondos,
	}
	channelValues := []models.Score{
		scores.CasaMatriz,
		scores.GestorCreditos,
	}
	geographyValues := []models.Score{
		scores.ZonaGeografica,
		scores.PresenciaDelitos,
		scores.PaisesGAFI,
		scores.Domicilio,
		scores.FondosTransferidos,
	}

	averageClient := round2(average(clientValues))
	averageChannels := round2(average(channelValues))
	averageGeography := round2(average(geographyValues))
	finalScore := round2(
		averageClient*0.80 +
			averageChannels*0.10 +
			averageGeography*0.10,
	)

	riskLevel := "Bajo"
	switch {
	case finalScore > 2:
		riskLevel = "Alto"
	case finalScore > 1:
		riskLevel = "Medio"
	}

	dueDiligence := "Estándar o Simplificada"
	if finalScore >= 2.1 {
		dueDiligence = "Ampliada"
	}

	return models.RiskSummary{
		AverageClient:    averageClient,
		AverageChannels:  averageChannels,
		AverageGeography: averageGeography,
		FinalScore:       finalScore,
		RiskLevel:        riskLevel,
		DueDiligence:     dueDiligence,
	}
}

func average(scores []models.Score) float64 {
	total := 0.0
	for _, score := range scores {
		total += score.Value
	}
	return total / float64(len(scores))
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}
