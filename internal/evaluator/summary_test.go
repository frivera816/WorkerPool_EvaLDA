package evaluator

import (
	"testing"
)

func TestSummarizeUsesPHPFormula(t *testing.T) {
	scores := defaultScores()
	scores.TipoCliente.Value = 2
	scores.Frecuencia.Value = 2
	scores.FormaPago.Value = 2
	scores.Nacionalidad.Value = 2
	scores.AniosRelacion.Value = 2
	scores.InformacionPerfil.Value = 2
	scores.PEP.Value = 2
	scores.APNFD.Value = 2
	scores.NoticiasNegativas.Value = 2
	scores.ProcedenciaFondos.Value = 2
	scores.CasaMatriz.Value = 3
	scores.GestorCreditos.Value = 1
	scores.ZonaGeografica.Value = 3
	scores.PresenciaDelitos.Value = 1
	scores.PaisesGAFI.Value = 2
	scores.Domicilio.Value = 2
	scores.FondosTransferidos.Value = 2

	summary := Summarize(scores)
	if summary.AverageClient != 2 {
		t.Fatalf("promedio cliente = %v", summary.AverageClient)
	}
	if summary.AverageChannels != 2 {
		t.Fatalf("promedio canales = %v", summary.AverageChannels)
	}
	if summary.AverageGeography != 2 {
		t.Fatalf("promedio geografía = %v", summary.AverageGeography)
	}
	if summary.FinalScore != 2 {
		t.Fatalf("calificación final = %v", summary.FinalScore)
	}
	if summary.RiskLevel != "Medio" {
		t.Fatalf("nivel = %q", summary.RiskLevel)
	}
	if summary.DueDiligence != "Estándar o Simplificada" {
		t.Fatalf("debida diligencia = %q", summary.DueDiligence)
	}
}

func TestSummarizeAmpliadaThreshold(t *testing.T) {
	scores := defaultScores()
	scores.TipoCliente.Value = 3
	scores.Frecuencia.Value = 3
	scores.FormaPago.Value = 3
	scores.Nacionalidad.Value = 3
	scores.AniosRelacion.Value = 3
	scores.InformacionPerfil.Value = 3
	scores.PEP.Value = 3
	scores.APNFD.Value = 3
	scores.NoticiasNegativas.Value = 3
	scores.ProcedenciaFondos.Value = 3
	scores.CasaMatriz.Value = 3
	scores.GestorCreditos.Value = 3
	scores.ZonaGeografica.Value = 3
	scores.PresenciaDelitos.Value = 3
	scores.PaisesGAFI.Value = 3
	scores.Domicilio.Value = 3
	scores.FondosTransferidos.Value = 3

	summary := Summarize(scores)
	if summary.FinalScore != 3 {
		t.Fatalf("calificación final = %v", summary.FinalScore)
	}
	if summary.RiskLevel != "Alto" {
		t.Fatalf("nivel = %q", summary.RiskLevel)
	}
	if summary.DueDiligence != "Ampliada" {
		t.Fatalf("debida diligencia = %q", summary.DueDiligence)
	}
}
