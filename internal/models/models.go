package models

// Contract conserva la fecha original YYYYMMDD de la fuente.
type Contract struct {
	Date   string
	Origin string
}

type Payment struct {
	Date   string
	Amount float64
	Cash   string
	Check  string
	Other  string
}

type Reference struct {
	Type             string
	Name             string
	Relationship     string
	AccountNumber    string
	WorkPhone        string
	InformationSince string
}

// ClientData contiene exclusivamente los datos necesarios para las reglas.
type ClientData struct {
	ID int64

	PEP          string
	DocumentType string

	FirstName string
	LastName  string

	BirthDate                string
	DUIExpeditionDate        string
	DUIRenewalDate           string
	Address                  string
	Profession               string
	MunicipalityID           int64
	ExpeditionMunicipalityID int64

	OriginOfFunds          string
	EconomicActivity       string
	PaymentForm            string
	OriginOfFundsDetail    string
	OtherIncomeSource      string
	OtherIncomeDetail      string
	BusinessAddress        string
	BusinessMunicipalityID int64

	CountryID    int64
	CountryKnown bool

	HasComplement       bool
	ComplementCreatedAt string
	BusinessStartDate   string
	DailySales          float64
	DailyCosts          float64
	MonthlyIncome       float64
	BusinessLocal       string
	JudicialRecord      string

	References map[string]Reference

	ImageCount  int64
	LastImageAt string

	Contracts []Contract
	Payments  []Payment
}

type ScoreSource uint8

const (
	SourceAutomatic ScoreSource = iota + 1
	SourceDefault
)

type Score struct {
	Code   string
	Name   string
	Value  float64
	Source ScoreSource
	Reason string
}

type Scores struct {
	TipoCliente        Score
	Frecuencia         Score
	FormaPago          Score
	Nacionalidad       Score
	AniosRelacion      Score
	InformacionPerfil  Score
	PEP                Score
	APNFD              Score
	NoticiasNegativas  Score
	ProcedenciaFondos  Score
	ZonaGeografica     Score
	PresenciaDelitos   Score
	PaisesGAFI         Score
	Domicilio          Score
	FondosTransferidos Score
	CasaMatriz         Score
	GestorCreditos     Score
}

func (s Scores) All() []Score {
	return []Score{
		s.TipoCliente,
		s.Frecuencia,
		s.FormaPago,
		s.Nacionalidad,
		s.AniosRelacion,
		s.InformacionPerfil,
		s.PEP,
		s.APNFD,
		s.NoticiasNegativas,
		s.ProcedenciaFondos,
		s.ZonaGeografica,
		s.PresenciaDelitos,
		s.PaisesGAFI,
		s.Domicilio,
		s.FondosTransferidos,
		s.CasaMatriz,
		s.GestorCreditos,
	}
}

type Evaluation struct {
	ClientID       int64
	Scores         Scores
	AutomaticCount int
	DefaultCount   int
}

// RiskSummary reproduce los promedios y la fórmula del guardado PHP.
type RiskSummary struct {
	AverageClient    float64
	AverageChannels  float64
	AverageGeography float64
	FinalScore       float64
	RiskLevel        string
	DueDiligence     string
}
