package evaluator

import (
	"math"
	"sort"
	"strings"
	"time"

	"worker_pool_eva/internal/models"
)

type Engine struct {
	today              time.Time
	contractsAvailable bool
}

func New(now time.Time, contractsAvailable bool) *Engine {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return &Engine{today: today, contractsAvailable: contractsAvailable}
}

func (e *Engine) Evaluate(client models.ClientData) models.Evaluation {
	scores := defaultScores()
	contracts := e.validContracts(client.Contracts)
	isPEP := isPEPValue(client.PEP)

	// A1: PEP, antecedentes judiciales o antigüedad del primer contrato.
	if isPEP {
		scores.TipoCliente = automatic(scores.TipoCliente, 3, "El cliente está marcado como PEP en SCC.")
	} else if hasJudicialRecord(client) {
		scores.TipoCliente = automatic(scores.TipoCliente, 3, "La marca de antecedentes judiciales está activa.")
	} else if len(contracts) > 0 {
		years, months, days := calendarDifference(parseDate(contracts[0].Date), e.today)
		if years > 5 || (years == 5 && (months > 0 || days > 0)) {
			scores.TipoCliente = automatic(scores.TipoCliente, 0, "El primer contrato válido tiene más de 5 años.")
		} else if years > 1 || (years == 1 && (months > 0 || days > 0)) {
			scores.TipoCliente = automatic(scores.TipoCliente, 0.5, "El primer contrato válido tiene más de 1 año y hasta 5 años.")
		} else {
			scores.TipoCliente = automatic(scores.TipoCliente, 1, "El primer contrato válido tiene 1 año o menos.")
		}
	}

	// A2 y A3 se calculan para todos los clientes, incluidos los PEP.
	if e.contractsAvailable {
		scores.Frecuencia = e.evaluateFrequency(contracts)
		firstContract := ""
		if len(contracts) > 0 {
			firstContract = contracts[0].Date
		}
		scores.FormaPago = e.evaluatePaymentMethod(client.Payments, firstContract)
	}

	// A4 y B4: nacionalidad y domicilio nacional parcial.
	e.evaluateNationalityAndDomicile(&scores, client)

	// A5: años de relación comercial desde el contrato más antiguo.
	if e.contractsAvailable && len(contracts) > 0 {
		scores.AniosRelacion = e.evaluateRelationshipYears(contracts[0].Date)
	}

	// A6: completitud y vigencia del perfil.
	scores.InformacionPerfil = automatic(scores.InformacionPerfil, e.profileScore(client),
		"Calculado con los campos, referencias, imágenes y fechas del perfil.")

	// A7: la marca PEP se evalúa siempre; ausente equivale a no PEP.
	if isPEP {
		scores.PEP = automatic(scores.PEP, 3, "La marca PEP del cliente está activa en SCC.")
	} else {
		scores.PEP = automatic(scores.PEP, 0, "La marca PEP del cliente no está activa o está vacía en SCC.")
	}

	// El PHP anula A1, A2, A3 y A5 cuando falta una fuente obligatoria.
	if !e.contractsAvailable {
		reason := "No está disponible una fuente obligatoria de contratos."
		scores.TipoCliente = withDefaultReason(scores.TipoCliente, reason)
		scores.Frecuencia = withDefaultReason(scores.Frecuencia, reason)
		scores.FormaPago = withDefaultReason(scores.FormaPago, reason)
		scores.AniosRelacion = withDefaultReason(scores.AniosRelacion, reason)
	}

	evaluation := models.Evaluation{ClientID: client.ID, Scores: scores}
	for _, score := range scores.All() {
		if score.Source == models.SourceAutomatic {
			evaluation.AutomaticCount++
		} else {
			evaluation.DefaultCount++
		}
	}
	return evaluation
}

func (e *Engine) evaluateFrequency(contracts []models.Contract) models.Score {
	start := time.Date(e.today.Year()-4, 1, 1, 0, 0, 0, 0, time.UTC).Format("20060102")
	end := e.today.Format("20060102")

	movements := make([]string, 0, len(contracts))
	byYear := make(map[int]int)
	for _, contract := range contracts {
		if contract.Date < start || contract.Date > end {
			continue
		}
		movements = append(movements, contract.Date)
		year := int(parseDate(contract.Date).Year())
		byYear[year]++
	}
	if len(movements) == 0 {
		return models.Score{
			Code: "A2", Name: "Frecuencia de la Relación Comercial",
			Value: 3, Source: models.SourceAutomatic,
			Reason: "No existen contratos dentro de la ventana de 5 años.",
		}
	}

	sort.Strings(movements)
	lastYear := int(parseDate(movements[len(movements)-1]).Year())

	// La continuidad se mide sobre todo el historial y termina en el último
	// año con actividad, para no exigir contratos en el año en curso y para
	// poder reconocer relaciones de más de 5 años consecutivos.
	allYears := make(map[int]bool)
	for _, contract := range contracts {
		allYears[int(parseDate(contract.Date).Year())] = true
	}
	consecutive := 0
	for year := lastYear; allYears[year]; year-- {
		consecutive++
	}

	// "Menos de 1 año" solo aplica si la relación efectivamente comenzó
	// hace un año o menos; si no, un único año de actividad se evalúa por
	// periodicidad.
	relationshipUpToOneYear := false
	if len(contracts) > 0 {
		first := parseDate(contracts[0].Date)
		if !first.IsZero() {
			relationshipUpToOneYear = !e.today.After(first.AddDate(1, 0, 0))
		}
	}

	switch {
	case consecutive >= 6:
		return models.Score{Code: "A2", Name: "Frecuencia de la Relación Comercial", Value: 0, Source: models.SourceAutomatic, Reason: "Actividad durante más de 5 años consecutivos."}
	case consecutive >= 2:
		return models.Score{Code: "A2", Name: "Frecuencia de la Relación Comercial", Value: 0.5, Source: models.SourceAutomatic, Reason: "Actividad durante más de 1 y hasta 5 años consecutivos."}
	case consecutive == 1 && relationshipUpToOneYear:
		return models.Score{Code: "A2", Name: "Frecuencia de la Relación Comercial", Value: 1, Source: models.SourceAutomatic, Reason: "Actividad durante menos de 1 año."}
	}

	maxPerYear := 0
	for _, count := range byYear {
		if count > maxPerYear {
			maxPerYear = count
		}
	}
	switch {
	case maxPerYear >= 12:
		return models.Score{Code: "A2", Name: "Frecuencia de la Relación Comercial", Value: 1.4, Source: models.SourceAutomatic, Reason: "12 o más contratos en un año."}
	case maxPerYear >= 6:
		return models.Score{Code: "A2", Name: "Frecuencia de la Relación Comercial", Value: 1.6, Source: models.SourceAutomatic, Reason: "Entre 6 y 11 contratos en un año."}
	case maxPerYear >= 2:
		return models.Score{Code: "A2", Name: "Frecuencia de la Relación Comercial", Value: 2, Source: models.SourceAutomatic, Reason: "Entre 2 y 5 contratos en un año."}
	case len(movements) <= 1:
		return models.Score{Code: "A2", Name: "Frecuencia de la Relación Comercial", Value: 3, Source: models.SourceAutomatic, Reason: "Un solo contrato en el período evaluado."}
	default:
		return models.Score{Code: "A2", Name: "Frecuencia de la Relación Comercial", Value: 3, Source: models.SourceAutomatic, Reason: "Sin condición de frecuencia identificada."}
	}
}

func (e *Engine) evaluatePaymentMethod(payments []models.Payment, firstContract string) models.Score {
	// Se analiza el comportamiento reciente (últimos 12 meses).
	startOfMonth := time.Date(e.today.Year(), e.today.Month(), 1, 0, 0, 0, 0, time.UTC)
	windowStart := startOfMonth.AddDate(0, -11, 0).Format("20060102")

	months := make(map[string]float64)
	methodCounts := make(map[string]int)

	for _, payment := range payments {
		if !e.dateValid(payment.Date) || payment.Date < windowStart || payment.Amount <= 0 {
			continue
		}
		month := payment.Date[:6]
		months[month] += payment.Amount
		if method := paymentMethod(payment); method != "" {
			methodCounts[method]++
		}
	}
	if len(months) == 0 {
		return withDefaultReason(defaultScores().FormaPago, "No existen abonos con fecha y monto válidos en los últimos 12 meses para evaluar.")
	}

	// El promedio se calcula sobre los meses del período analizado, no solo
	// sobre los meses que tuvieron abonos. Si la relación comenzó dentro de
	// la ventana, se usan los meses transcurridos desde su inicio.
	monthsAnalyzed := 12
	if firstContract != "" {
		if start := parseDate(firstContract); !start.IsZero() {
			relationshipMonths := ((startOfMonth.Year()-start.Year())*12 +
				(int(startOfMonth.Month()) - int(start.Month()))) + 1
			if relationshipMonths >= 1 {
				monthsAnalyzed = relationshipMonths
				if monthsAnalyzed > 12 {
					monthsAnalyzed = 12
				}
			}
		}
	}

	total := 0.0
	for _, amount := range months {
		total += amount
	}
	average := math.Round((total/float64(monthsAnalyzed))*100) / 100

	maxCount := 0
	for _, count := range methodCounts {
		if count > maxCount {
			maxCount = count
		}
	}
	if maxCount == 0 {
		return withDefaultReason(defaultScores().FormaPago, "Los abonos no tienen una forma de pago reconocible.")
	}
	winner := ""
	winners := 0
	for method, count := range methodCounts {
		if count == maxCount {
			winner = method
			winners++
		}
	}
	if winners != 1 {
		return withDefaultReason(defaultScores().FormaPago, "No existe un método de pago predominante.")
	}

	value, ok := classifyPayment(winner, average)
	if !ok {
		return withDefaultReason(defaultScores().FormaPago, "El promedio mensual o el método queda fuera de los rangos definidos.")
	}
	return models.Score{
		Code: "A3", Name: "Forma de Pago", Value: value,
		Source: models.SourceAutomatic,
		Reason: "Calculado con el promedio mensual de abonos de los últimos 12 meses y el método de pago predominante.",
	}
}

func (e *Engine) evaluateNationalityAndDomicile(scores *models.Scores, client models.ClientData) {
	documentType := strings.ToLower(strings.TrimSpace(client.DocumentType))
	nonDUI := documentType != "" && documentType != "dui"

	if nonDUI {
		scores.Nacionalidad = automatic(scores.Nacionalidad, 1.5,
			"Documento distinto de DUI; extranjero residente con país de origen no determinado.")
	}

	if !client.CountryKnown {
		if !nonDUI {
			scores.Nacionalidad = withDefaultReason(scores.Nacionalidad,
				"No fue posible determinar el país mediante el municipio del cliente.")
		}
		return
	}

	if client.CountryID == 1 {
		if !nonDUI {
			scores.Nacionalidad = automatic(scores.Nacionalidad, 0,
				"El país asociado al municipio del cliente es El Salvador.")
		}
		scores.Domicilio = automatic(scores.Domicilio, 0,
			"El país asociado al municipio del cliente es El Salvador.")
		return
	}

	if !nonDUI {
		scores.Nacionalidad = automatic(scores.Nacionalidad, 2,
			"El país asociado al municipio del cliente es distinto de El Salvador.")
	}
	// El domicilio extranjero no se automatiza y conserva su valor predeterminado.
}

func (e *Engine) evaluateRelationshipYears(firstContract string) models.Score {
	from := parseDate(firstContract)
	oneYear := from.AddDate(1, 0, 0)
	threeYears := from.AddDate(3, 0, 0)

	switch {
	case !e.today.After(oneYear):
		return models.Score{Code: "A5", Name: "Años de Relación Comercial", Value: 3, Source: models.SourceAutomatic, Reason: "El contrato válido más antiguo tiene 1 año o menos."}
	case e.today.Before(threeYears):
		return models.Score{Code: "A5", Name: "Años de Relación Comercial", Value: 2, Source: models.SourceAutomatic, Reason: "El contrato válido más antiguo tiene más de 1 año y menos de 3 años."}
	case e.today.Equal(threeYears):
		return withDefaultReason(defaultScores().AniosRelacion, "La relación cumple exactamente 3 años, caso no definido en el formulario.")
	default:
		return models.Score{Code: "A5", Name: "Años de Relación Comercial", Value: 1, Source: models.SourceAutomatic, Reason: "El contrato válido más antiguo tiene más de 3 años."}
	}
}

func (e *Engine) profileScore(client models.ClientData) float64 {
	general := []bool{
		textInformed(client.FirstName),
		textInformed(client.LastName),
		e.dateValid(client.BirthDate),
		e.dateValid(client.DUIExpeditionDate),
		e.dateStructureValid(client.DUIRenewalDate),
		textInformed(client.Address),
		textInformed(client.Profession),
		client.MunicipalityID > 0,
		client.ExpeditionMunicipalityID > 0,
	}
	legal := []bool{client.ImageCount > 0}
	financial := []bool{
		textInformed(client.OriginOfFunds),
		textInformed(client.EconomicActivity),
		textInformed(client.PaymentForm),
		textInformed(client.OriginOfFundsDetail),
		client.MonthlyIncome > 0,
	}
	if client.OtherIncomeSource == "S" {
		financial = append(financial, textInformed(client.OtherIncomeDetail))
	}
	business := []bool{
		textInformed(client.BusinessAddress),
		client.BusinessMunicipalityID > 0,
		e.dateValid(client.BusinessStartDate),
		client.DailySales > 0,
		client.DailyCosts > 0,
		textInformed(client.BusinessLocal),
	}

	family := client.References["familiar"]
	personal := client.References["particular"]
	financialRef := client.References["financiera"]
	commercial := client.References["comercial"]
	references := []bool{
		textInformed(family.Name) && textInformed(family.Relationship),
		textInformed(personal.Name),
		textInformed(financialRef.Name) && textInformed(financialRef.AccountNumber),
		textInformed(commercial.Name) && textInformed(commercial.WorkPhone),
	}

	total := 0
	completed := 0
	for _, block := range [][]bool{general, legal, financial, business, references} {
		total += len(block)
		for _, item := range block {
			if item {
				completed++
			}
		}
	}

	emptyBlocks := 0
	for _, block := range [][]bool{legal, financial, business, references} {
		blockCompleted := 0
		for _, item := range block {
			if item {
				blockCompleted++
			}
		}
		if blockCompleted == 0 {
			emptyBlocks++
		}
	}

	outdated := false
	if days, ok := e.daysSince(client.ComplementCreatedAt); ok && days > 183 {
		outdated = true
	}
	if days, ok := e.daysSince(client.LastImageAt); ok && days > 183 {
		outdated = true
	}
	for _, reference := range client.References {
		if days, ok := e.daysSince(reference.InformationSince); ok && days > 183 {
			outdated = true
		}
	}

	switch {
	case emptyBlocks == 4:
		return 3
	case completed == total && !outdated:
		return 0
	case completed == total:
		return 1
	default:
		return 2
	}
}

func (e *Engine) validContracts(contracts []models.Contract) []models.Contract {
	valid := make([]models.Contract, 0, len(contracts))
	for _, contract := range contracts {
		if e.dateValid(contract.Date) {
			valid = append(valid, contract)
		}
	}
	sort.SliceStable(valid, func(i, j int) bool {
		return valid[i].Date < valid[j].Date
	})
	return valid
}

func (e *Engine) dateStructureValid(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 8 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	_, err := time.ParseInLocation("20060102", value, time.UTC)
	return err == nil
}

func (e *Engine) dateValid(value string) bool {
	value = strings.TrimSpace(value)
	if !e.dateStructureValid(value) {
		return false
	}
	return value <= e.today.Format("20060102")
}

func (e *Engine) daysSince(value string) (int, bool) {
	var digits strings.Builder
	for _, character := range value {
		if character >= '0' && character <= '9' {
			digits.WriteRune(character)
		}
	}
	normalized := digits.String()
	if len(normalized) < 8 {
		return 0, false
	}
	normalized = normalized[:8]
	if !e.dateValid(normalized) {
		return 0, false
	}
	date := parseDate(normalized)
	return int(e.today.Sub(date).Hours() / 24), true
}

func defaultScores() models.Scores {
	return models.Scores{
		TipoCliente:        fallback("A1", "Tipo de Cliente", 1, "No existe un primer contrato válido para automatizar la evaluación."),
		Frecuencia:         fallback("A2", "Frecuencia de la Relación Comercial", 1, "No fue posible determinar automáticamente la frecuencia."),
		FormaPago:          fallback("A3", "Forma de Pago", 0.5, "No fue posible determinar automáticamente la forma de pago."),
		Nacionalidad:       fallback("A4", "Nacionalidad", 0, "No fue posible determinar automáticamente la nacionalidad."),
		AniosRelacion:      fallback("A5", "Años de Relación Comercial", 3, "No fue posible determinar automáticamente los años de relación."),
		InformacionPerfil:  fallback("A6", "Información actualizada del Perfil", 0, "No fue posible determinar automáticamente la vigencia del perfil."),
		PEP:                fallback("A7", "PEP", 0, "No fue posible determinar automáticamente la marca PEP."),
		APNFD:              fallback("A8", "APNFD", 0, "No existe un catálogo APNFD para clasificar la actividad del cliente."),
		NoticiasNegativas:  fallback("A9", "Noticias negativas", 0, "No existe una fuente de noticias negativas del cliente."),
		ProcedenciaFondos:  fallback("A10", "Procedencia de fondos", 0, "No existe información suficiente para verificar la procedencia de los pagos."),
		ZonaGeografica:     fallback("B1", "Zona geográfica de ubicación", 1, "No existe una fuente de índices de criminalidad ni una clasificación de zonas rurales."),
		PresenciaDelitos:   fallback("B2", "Presencia de delitos relacionados con LDA/FT/FPADM", 1, "No existe una fuente de delitos asociados a la ubicación del cliente."),
		PaisesGAFI:         fallback("B3", "Países objetados por GAFI", 0, "No existe un catálogo ni historial de países objetados por GAFI."),
		Domicilio:          fallback("B4", "Domicilio del cliente", 0, "Solo se automatiza cuando el domicilio es nacional."),
		FondosTransferidos: fallback("B5", "Fondos transferidos de países en listas negras", 0, "No existe información del país de origen de las transferencias."),
		CasaMatriz:         fallback("C1", "Casa Matriz", 1, "No existe información de criminalidad por ciudad."),
		GestorCreditos:     fallback("C2", "Gestor de Créditos", 1, "No existe información de criminalidad por zona de cobro."),
	}
}

func fallback(code, name string, value float64, reason string) models.Score {
	return models.Score{Code: code, Name: name, Value: value, Source: models.SourceDefault, Reason: reason}
}

func automatic(score models.Score, value float64, reason string) models.Score {
	score.Value = value
	score.Source = models.SourceAutomatic
	score.Reason = reason
	return score
}

func withDefaultReason(score models.Score, reason string) models.Score {
	score.Value = defaultValue(score.Code)
	score.Source = models.SourceDefault
	score.Reason = reason
	return score
}

func defaultValue(code string) float64 {
	for _, score := range defaultScores().All() {
		if score.Code == code {
			return score.Value
		}
	}
	return 0
}

// isPEPValue reconoce la marca PEP de SCC (char(1) con 's'/'n') y también
// valores antiguos como '1' o 'si'.
func isPEPValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "s", "1", "si":
		return true
	}
	return false
}

func hasJudicialRecord(client models.ClientData) bool {
	if !client.HasComplement {
		return false
	}
	record := strings.TrimSpace(client.JudicialRecord)
	return record == "1" || strings.ToUpper(record) == "S"
}

func textInformed(value string) bool {
	return strings.TrimSpace(value) != ""
}

func paymentMethod(payment models.Payment) string {
	cash := strings.ToLower(strings.TrimSpace(payment.Cash))
	check := strings.TrimSpace(payment.Check)
	other := strings.ToLower(strings.TrimSpace(payment.Other))

	if cash != "" && cash != "0" && cash != "no" {
		return "Efectivo"
	}
	if check != "" && check != "0" {
		return "Cheque/Transferencia"
	}
	if other != "" && other != "0" {
		return "Otro"
	}
	return ""
}

func classifyPayment(method string, monthlyAverage float64) (float64, bool) {
	if monthlyAverage < 0 {
		return 0, false
	}
	switch method {
	case "Efectivo":
		switch {
		case monthlyAverage < 500:
			return 0, true
		case monthlyAverage < 2000:
			return 0.5, true
		case monthlyAverage < 10000:
			return 1, true
		case monthlyAverage > 25000:
			return 3, true
		default:
			return 0, false
		}
	case "Cheque/Transferencia":
		switch {
		case monthlyAverage < 2000:
			return 1.5, true
		case monthlyAverage < 10000:
			return 1.8, true
		case monthlyAverage <= 25000:
			return 2, true
		default:
			return 0, false
		}
	default:
		return 0, false
	}
}

func parseDate(value string) time.Time {
	value = strings.TrimSpace(value)
	parsed, err := time.ParseInLocation("20060102", value, time.UTC)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func calendarDifference(from, to time.Time) (years, months, days int) {
	if from.IsZero() || from.After(to) {
		return 0, 0, 0
	}
	anchor := from
	for {
		next := anchor.AddDate(1, 0, 0)
		if next.After(to) {
			break
		}
		years++
		anchor = next
	}
	for months < 11 {
		next := anchor.AddDate(0, 1, 0)
		if next.After(to) {
			break
		}
		months++
		anchor = next
	}
	days = int(to.Sub(anchor).Hours() / 24)
	return years, months, days
}
