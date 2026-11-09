package evaluator

import (
	"strings"
	"testing"
	"time"

	"worker_pool_eva/internal/models"
)

func testEngine() *Engine {
	now := time.Date(2026, 11, 6, 15, 30, 0, 0, time.UTC)
	return New(now, true)
}

func TestDefaultFallbacksAndSources(t *testing.T) {
	evaluation := testEngine().Evaluate(models.ClientData{ID: 1})

	expectScore(t, evaluation.Scores.TipoCliente, 1, models.SourceDefault)
	expectScore(t, evaluation.Scores.Frecuencia, 3, models.SourceAutomatic)
	expectScore(t, evaluation.Scores.FormaPago, 0.5, models.SourceDefault)
	expectScore(t, evaluation.Scores.Nacionalidad, 0, models.SourceDefault)
	expectScore(t, evaluation.Scores.AniosRelacion, 3, models.SourceDefault)
	expectScore(t, evaluation.Scores.InformacionPerfil, 3, models.SourceAutomatic)
	expectScore(t, evaluation.Scores.PEP, 0, models.SourceAutomatic)
	expectScore(t, evaluation.Scores.APNFD, 0, models.SourceDefault)
	expectScore(t, evaluation.Scores.NoticiasNegativas, 0, models.SourceDefault)
	expectScore(t, evaluation.Scores.ProcedenciaFondos, 0, models.SourceDefault)
	expectScore(t, evaluation.Scores.ZonaGeografica, 1, models.SourceDefault)
	expectScore(t, evaluation.Scores.PresenciaDelitos, 1, models.SourceDefault)
	expectScore(t, evaluation.Scores.PaisesGAFI, 0, models.SourceDefault)
	expectScore(t, evaluation.Scores.Domicilio, 0, models.SourceDefault)
	expectScore(t, evaluation.Scores.FondosTransferidos, 0, models.SourceDefault)
	expectScore(t, evaluation.Scores.CasaMatriz, 1, models.SourceDefault)
	expectScore(t, evaluation.Scores.GestorCreditos, 1, models.SourceDefault)

	if evaluation.AutomaticCount != 3 || evaluation.DefaultCount != 14 {
		t.Fatalf("automáticas=%d, predeterminadas=%d", evaluation.AutomaticCount, evaluation.DefaultCount)
	}
}

func TestMissingContractSourceForcesFourFallbacks(t *testing.T) {
	engine := New(time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC), false)
	evaluation := engine.Evaluate(models.ClientData{
		ID:        1,
		PEP:       "1",
		Contracts: []models.Contract{{Date: "20200101", Origin: "normal"}},
	})

	expectScore(t, evaluation.Scores.TipoCliente, 1, models.SourceDefault)
	expectScore(t, evaluation.Scores.Frecuencia, 1, models.SourceDefault)
	expectScore(t, evaluation.Scores.FormaPago, 0.5, models.SourceDefault)
	expectScore(t, evaluation.Scores.AniosRelacion, 3, models.SourceDefault)
	expectScore(t, evaluation.Scores.PEP, 3, models.SourceAutomatic)
}

func TestPEPRules(t *testing.T) {
	for _, value := range []string{"1", "s", "S", "si", " SI "} {
		t.Run("PEP "+value, func(t *testing.T) {
			evaluation := testEngine().Evaluate(models.ClientData{ID: 1, PEP: value})

			expectScore(t, evaluation.Scores.TipoCliente, 3, models.SourceAutomatic)
			// A2 y A3 también se calculan para PEP: sin contratos ni abonos
			// aplican sus valores automáticos y predeterminados normales.
			expectScore(t, evaluation.Scores.Frecuencia, 3, models.SourceAutomatic)
			expectScore(t, evaluation.Scores.FormaPago, 0.5, models.SourceDefault)
			expectScore(t, evaluation.Scores.PEP, 3, models.SourceAutomatic)
		})
	}
}

func TestPEPPaysAttentionToContractActivity(t *testing.T) {
	evaluation := testEngine().Evaluate(models.ClientData{
		ID:  1,
		PEP: "s",
		Contracts: []models.Contract{
			{Date: "20250101", Origin: "normal"},
			{Date: "20260101", Origin: "normal"},
		},
		Payments: []models.Payment{{Date: "20261101", Amount: 6000, Cash: "1"}},
	})

	expectScore(t, evaluation.Scores.TipoCliente, 3, models.SourceAutomatic)
	expectScore(t, evaluation.Scores.Frecuencia, 0.5, models.SourceAutomatic)
	if evaluation.Scores.FormaPago.Source != models.SourceAutomatic {
		t.Fatalf("A3 en cliente PEP debería calcularse automáticamente: %s", evaluation.Scores.FormaPago.Reason)
	}
	expectScore(t, evaluation.Scores.PEP, 3, models.SourceAutomatic)
}

func TestJudicialRecordRule(t *testing.T) {
	evaluation := testEngine().Evaluate(models.ClientData{
		ID:                  1,
		HasComplement:       true,
		JudicialRecord:      "s",
		ComplementCreatedAt: "20261001",
	})

	expectScore(t, evaluation.Scores.TipoCliente, 3, models.SourceAutomatic)
}

func TestNationalityAndDomicile(t *testing.T) {
	national := testEngine().Evaluate(models.ClientData{
		ID: 1, DocumentType: "DUI", CountryKnown: true, CountryID: 1,
	})
	expectScore(t, national.Scores.Nacionalidad, 0, models.SourceAutomatic)
	expectScore(t, national.Scores.Domicilio, 0, models.SourceAutomatic)

	foreign := testEngine().Evaluate(models.ClientData{
		ID: 2, DocumentType: "DUI", CountryKnown: true, CountryID: 2,
	})
	expectScore(t, foreign.Scores.Nacionalidad, 2, models.SourceAutomatic)
	expectScore(t, foreign.Scores.Domicilio, 0, models.SourceDefault)

	resident := testEngine().Evaluate(models.ClientData{
		ID: 3, DocumentType: "Pasaporte", CountryKnown: true, CountryID: 2,
	})
	expectScore(t, resident.Scores.Nacionalidad, 1.5, models.SourceAutomatic)
	expectScore(t, resident.Scores.Domicilio, 0, models.SourceDefault)
}

func TestContractAgeAndRelationshipYears(t *testing.T) {
	tests := []struct {
		name          string
		firstContract string
		wantA1        float64
		wantA5        float64
		wantA5Source  models.ScoreSource
	}{
		{"more than five years", "20190101", 0, 1, models.SourceAutomatic},
		{"exactly five years", "20211106", 0.5, 1, models.SourceAutomatic},
		{"between one and three years", "20250101", 0.5, 2, models.SourceAutomatic},
		{"exactly one year", "20251106", 1, 3, models.SourceAutomatic},
		{"exactly three years", "20231106", 0.5, 3, models.SourceDefault},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evaluation := testEngine().Evaluate(models.ClientData{
				ID:        1,
				Contracts: []models.Contract{{Date: test.firstContract, Origin: "normal"}},
			})
			expectScore(t, evaluation.Scores.TipoCliente, test.wantA1, models.SourceAutomatic)
			expectScore(t, evaluation.Scores.AniosRelacion, test.wantA5, test.wantA5Source)
		})
	}
}

func TestFrequencyUsesFiveYearWindow(t *testing.T) {
	evaluation := testEngine().Evaluate(models.ClientData{
		ID: 1,
		Contracts: []models.Contract{
			{Date: "20250101", Origin: "normal"},
			{Date: "20260101", Origin: "normal"},
		},
	})
	expectScore(t, evaluation.Scores.Frecuencia, 0.5, models.SourceAutomatic)

	oldOnly := testEngine().Evaluate(models.ClientData{
		ID:        2,
		Contracts: []models.Contract{{Date: "20190101", Origin: "normal"}},
	})
	expectScore(t, oldOnly.Scores.Frecuencia, 3, models.SourceAutomatic)
}

func TestFrequencyUsesFullHistoryForContinuity(t *testing.T) {
	// La continuidad termina en el último año con actividad, aunque no haya
	// contratos en el año en curso.
	withoutCurrentYear := testEngine().Evaluate(models.ClientData{
		ID: 1,
		Contracts: []models.Contract{
			{Date: "20220101", Origin: "normal"},
			{Date: "20230101", Origin: "normal"},
			{Date: "20240101", Origin: "normal"},
			{Date: "20250101", Origin: "normal"},
		},
	})
	expectScore(t, withoutCurrentYear.Scores.Frecuencia, 0.5, models.SourceAutomatic)

	// Más de cinco años consecutivos usando todo el historial, aunque la
	// ventana de cinco años solo cubra los últimos años.
	moreThanFiveYears := testEngine().Evaluate(models.ClientData{
		ID: 2,
		Contracts: []models.Contract{
			{Date: "20180101", Origin: "normal"},
			{Date: "20190101", Origin: "normal"},
			{Date: "20200101", Origin: "normal"},
			{Date: "20210101", Origin: "normal"},
			{Date: "20220101", Origin: "normal"},
			{Date: "20230101", Origin: "normal"},
			{Date: "20240101", Origin: "normal"},
		},
	})
	expectScore(t, moreThanFiveYears.Scores.Frecuencia, 0, models.SourceAutomatic)
}

func TestFrequencyDistinguishesNewAndOldSingleYear(t *testing.T) {
	newRelationship := testEngine().Evaluate(models.ClientData{
		ID:        1,
		Contracts: []models.Contract{{Date: "20260101", Origin: "normal"}},
	})
	expectScore(t, newRelationship.Scores.Frecuencia, 1, models.SourceAutomatic)

	oldSingleYear := testEngine().Evaluate(models.ClientData{
		ID:        2,
		Contracts: []models.Contract{{Date: "20230101", Origin: "normal"}},
	})
	expectScore(t, oldSingleYear.Scores.Frecuencia, 3, models.SourceAutomatic)
	if !strings.Contains(oldSingleYear.Scores.Frecuencia.Reason, "Un solo contrato") {
		t.Fatalf("un año de actividad de una relación antigua debería evaluarse por periodicidad: %s", oldSingleYear.Scores.Frecuencia.Reason)
	}
}

func TestPaymentMethodClassification(t *testing.T) {
	tests := []struct {
		name       string
		payment    models.Payment
		wantValue  float64
		wantSource models.ScoreSource
	}{
		{"cash 500", models.Payment{Date: "20261101", Amount: 500, Cash: "1"}, 0.5, models.SourceAutomatic},
		{"cash 10000 undefined", models.Payment{Date: "20261101", Amount: 10000, Cash: "1"}, 0.5, models.SourceDefault},
		{"cash 25000 undefined", models.Payment{Date: "20261101", Amount: 25000, Cash: "1"}, 0.5, models.SourceDefault},
		{"cash above 25000", models.Payment{Date: "20261101", Amount: 25001, Cash: "1"}, 3, models.SourceAutomatic},
		{"check 1999", models.Payment{Date: "20261101", Amount: 1999, Check: "1"}, 1.5, models.SourceAutomatic},
		{"check 25000", models.Payment{Date: "20261101", Amount: 25000, Check: "1"}, 2, models.SourceAutomatic},
		{"check above 25000", models.Payment{Date: "20261101", Amount: 25001, Check: "1"}, 0.5, models.SourceDefault},
		{"other method", models.Payment{Date: "20261101", Amount: 100, Other: "1"}, 0.5, models.SourceDefault},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// El contrato inicia en el mes actual, por lo que el promedio se
			// divide entre un solo mes y coincide con el monto del abono.
			evaluation := testEngine().Evaluate(models.ClientData{
				ID:        1,
				Contracts: []models.Contract{{Date: "20261101", Origin: "normal"}},
				Payments:  []models.Payment{test.payment},
			})
			expectScore(t, evaluation.Scores.FormaPago, test.wantValue, test.wantSource)
		})
	}
}

func TestPaymentMethodTieFallsBack(t *testing.T) {
	evaluation := testEngine().Evaluate(models.ClientData{
		ID:        1,
		Contracts: []models.Contract{{Date: "20261101", Origin: "normal"}},
		Payments: []models.Payment{
			{Date: "20261101", Amount: 100, Cash: "1"},
			{Date: "20261102", Amount: 100, Check: "1"},
		},
	})
	expectScore(t, evaluation.Scores.FormaPago, 0.5, models.SourceDefault)
}

func TestPaymentMethodIgnoresPaymentsOutsideLastYear(t *testing.T) {
	evaluation := testEngine().Evaluate(models.ClientData{
		ID:        1,
		Contracts: []models.Contract{{Date: "20200101", Origin: "normal"}},
		Payments: []models.Payment{
			{Date: "20251101", Amount: 500, Cash: "1"},
			{Date: "20241101", Amount: 500, Cash: "1"},
		},
	})
	expectScore(t, evaluation.Scores.FormaPago, 0.5, models.SourceDefault)
}

func TestPaymentMethodAveragesOverRelationshipMonths(t *testing.T) {
	// Relación iniciada en septiembre de 2026: septiembre, octubre y noviembre
	// son tres meses analizados. Un abono de 900 se promedia como 300, que en
	// efectivo corresponde a la calificación 0.
	evaluation := testEngine().Evaluate(models.ClientData{
		ID:        1,
		Contracts: []models.Contract{{Date: "20260915", Origin: "normal"}},
		Payments:  []models.Payment{{Date: "20260920", Amount: 900, Cash: "1"}},
	})
	expectScore(t, evaluation.Scores.FormaPago, 0, models.SourceAutomatic)

	// Relación anterior a la ventana: se divide entre los 12 meses completos.
	// Un abono de 9000 se promedia como 750.
	longRelationship := testEngine().Evaluate(models.ClientData{
		ID:        2,
		Contracts: []models.Contract{{Date: "20200101", Origin: "normal"}},
		Payments:  []models.Payment{{Date: "20260920", Amount: 9000, Cash: "1"}},
	})
	expectScore(t, longRelationship.Scores.FormaPago, 0.5, models.SourceAutomatic)
}

func TestProfileScore(t *testing.T) {
	complete := models.ClientData{
		ID:                       1,
		FirstName:                "Nombre",
		LastName:                 "Apellido",
		BirthDate:                "19900101",
		DUIExpeditionDate:        "20200101",
		DUIRenewalDate:           "20300101",
		Address:                  "Dirección",
		Profession:               "Oficio",
		MunicipalityID:           1,
		ExpeditionMunicipalityID: 1,
		OriginOfFunds:            "Salario",
		EconomicActivity:         "Comercio",
		PaymentForm:              "Efectivo",
		OriginOfFundsDetail:      "Detalle",
		OtherIncomeSource:        "N",
		BusinessAddress:          "Negocio",
		BusinessMunicipalityID:   1,
		DailySales:               10,
		DailyCosts:               5,
		MonthlyIncome:            100,
		BusinessLocal:            "Local",
		ImageCount:               1,
		LastImageAt:              "20261001",
		ComplementCreatedAt:      "20261001",
		BusinessStartDate:        "20200101",
		References: map[string]models.Reference{
			"familiar":   {Name: "Familiar", Relationship: "Madre", InformationSince: "20261001"},
			"particular": {Name: "Personal", InformationSince: "20261001"},
			"financiera": {Name: "Financiera", AccountNumber: "123", InformationSince: "20261001"},
			"comercial":  {Name: "Comercial", WorkPhone: "123", InformationSince: "20261001"},
		},
	}

	current := testEngine().Evaluate(complete)
	expectScore(t, current.Scores.InformacionPerfil, 0, models.SourceAutomatic)

	stale := complete
	stale.ComplementCreatedAt = "20250101"
	expectScore(t, testEngine().Evaluate(stale).Scores.InformacionPerfil, 1, models.SourceAutomatic)

	incomplete := complete
	incomplete.LastName = ""
	incomplete.ImageCount = 0
	expectScore(t, testEngine().Evaluate(incomplete).Scores.InformacionPerfil, 2, models.SourceAutomatic)
}

func TestDateValidation(t *testing.T) {
	engine := testEngine()
	if !engine.dateStructureValid("20240229") {
		t.Fatal("2024-02-29 debería ser una fecha válida")
	}
	if engine.dateStructureValid("20230229") {
		t.Fatal("2023-02-29 no debería ser válida")
	}
	if engine.dateValid("20261107") {
		t.Fatal("una fecha futura no debería ser válida para el perfil")
	}
	if !engine.dateStructureValid("20300101") {
		t.Fatal("la estructura del vencimiento sí debe aceptar fechas futuras")
	}
	if days, ok := engine.daysSince("20261001 12:30:00"); !ok || days != 36 {
		t.Fatalf("daysSince = %d, %v", days, ok)
	}
}

func expectScore(t *testing.T, score models.Score, value float64, source models.ScoreSource) {
	t.Helper()
	if score.Value != value {
		t.Fatalf("%s: valor=%v, esperado=%v (%s)", score.Code, score.Value, value, score.Reason)
	}
	if score.Source != source {
		t.Fatalf("%s: origen=%v, esperado=%v (%s)", score.Code, score.Source, source, score.Reason)
	}
}
