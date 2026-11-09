package loader

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"worker_pool_eva/internal/database"
	"worker_pool_eva/internal/models"
)

type Loader struct {
	db     *database.DB
	tables database.TableAvailability
}

func New(db *database.DB, tables database.TableAvailability) *Loader {
	return &Loader{db: db, tables: tables}
}

// LoadBatch loads all data required by the evaluator using grouped queries.
func (l *Loader) LoadBatch(ctx context.Context, ids []int64) ([]models.ClientData, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	byID, err := l.loadClients(ctx, ids)
	if err != nil {
		return nil, err
	}
	if err := l.loadComplements(ctx, ids, byID); err != nil {
		return nil, err
	}
	if err := l.loadReferences(ctx, ids, byID); err != nil {
		return nil, err
	}
	if err := l.loadImages(ctx, ids, byID); err != nil {
		return nil, err
	}

	contractSources := []struct {
		table  string
		origin string
	}{
		{database.TableContractsMarkets, "mercados"},
		{database.TableContractsHP, "hp"},
		{database.TableContractsStock, "inventarios"},
		{database.TableContracts, "normal"},
	}
	for _, source := range contractSources {
		if !l.tables.Has(source.table) {
			continue
		}
		if err := l.loadContracts(ctx, ids, byID, source.table, source.origin); err != nil {
			return nil, err
		}
	}

	if l.tables.Has(database.TableContracts) && l.tables.Has(database.TablePayments) {
		if err := l.loadPayments(ctx, ids, byID, database.TablePayments, "a.valor"); err != nil {
			return nil, err
		}
	}
	if l.tables.Has(database.TableContractsHP) && l.tables.Has(database.TablePaymentsHP) {
		if err := l.loadPayments(ctx, ids, byID, database.TablePaymentsHP, "a.cantidad"); err != nil {
			return nil, err
		}
	}

	result := make([]models.ClientData, 0, len(byID))
	for _, id := range ids {
		if client, ok := byID[id]; ok {
			result = append(result, *client)
		}
	}
	return result, nil
}

func (l *Loader) loadClients(ctx context.Context, ids []int64) (map[int64]*models.ClientData, error) {
	countryColumn := "NULL AS id_pais"
	geoJoins := ""
	if l.tables.GeoAvailable() {
		countryColumn = "p.id_pais"
		geoJoins = `
LEFT JOIN empresas.emp_municipios m ON m.id_municipio = c.id_municipio
LEFT JOIN empresas.emp_departamentos d ON d.id_departamento = m.id_departamento
LEFT JOIN empresas.emp_paises p ON p.id_pais = d.id_pais`
	}

	juramentoColumns := `
    NULL AS origen_fondos,
    NULL AS actividad_economica,
    NULL AS forma_pago,
    NULL AS detalle_origen_procedencia,
    NULL AS otra_fuente_ingreso,
    NULL AS detalle_otros_ingresos,
    NULL AS direccion_negocio,
    NULL AS id_municipio_neg`
	juramentoJoin := ""
	args := make([]any, 0, len(ids)*2)

	if l.tables.Has(database.TableJuramento) {
		juramentoColumns = `
    j.origen_fondos,
    j.actividad_economica,
    j.forma_pago,
    j.detalle_origen_procedencia,
    j.otra_fuente_ingreso,
    j.detalle_otros_ingresos,
    j.direccion_negocio,
    j.id_municipio_neg`
		juramentoJoin = `
LEFT JOIN (
    SELECT
        j.id_cliente,
        j.id_cliente_juramento,
        j.origen_fondos,
        j.actividad_economica,
        j.forma_pago,
        j.detalle_origen_procedencia,
        j.otra_fuente_ingreso,
        j.detalle_otros_ingresos,
        j.direccion_negocio,
        j.id_municipio_neg
    FROM scc_creditos_clientes_juramento j
    INNER JOIN (
        SELECT id_cliente, MAX(id_cliente_juramento) AS max_id
        FROM scc_creditos_clientes_juramento
        WHERE id_cliente IN (` + placeholders(len(ids)) + `)
        GROUP BY id_cliente
    ) latest ON latest.max_id = j.id_cliente_juramento
) j ON j.id_cliente = c.id_cliente`
		// The derived juramento subquery appears before the outer IN clause.
		args = append(args, int64Args(ids)...)
	}
	args = append(args, int64Args(ids)...)

	query := `
SELECT
    c.id_cliente,
    c.pep,
    c.tipo_documento,
    c.nombre,
    c.apellido,
    c.fecha_nacimiento,
    c.fecha_expedicion_dui,
    c.fecha_vence_dui,
    c.direccion,
    c.profesion_oficio,
    c.id_municipio,
    c.id_municipio_expedicion,` + juramentoColumns + `,
    ` + countryColumn + `
FROM scc_creditos_clientes c` + geoJoins + juramentoJoin + `
WHERE c.id_cliente IN (` + placeholders(len(ids)) + `)
ORDER BY c.id_cliente`

	rows, err := l.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("no se pudieron cargar clientes, país y juramento: %w", err)
	}
	defer rows.Close()

	clients := make(map[int64]*models.ClientData, len(ids))
	for rows.Next() {
		var (
			id                        int64
			pep, documentType         sql.NullString
			firstName, lastName       sql.NullString
			birthDate, expeditionDate sql.NullString
			renewalDate, address      sql.NullString
			profession                sql.NullString
			municipalityID            sql.NullInt64
			expeditionMunicipalityID  sql.NullInt64
			originOfFunds             sql.NullString
			economicActivity          sql.NullString
			paymentForm               sql.NullString
			originOfFundsDetail       sql.NullString
			otherIncomeSource         sql.NullString
			otherIncomeDetail         sql.NullString
			businessAddress           sql.NullString
			businessMunicipalityID    sql.NullInt64
			countryID                 sql.NullInt64
		)
		err := rows.Scan(
			&id,
			&pep,
			&documentType,
			&firstName,
			&lastName,
			&birthDate,
			&expeditionDate,
			&renewalDate,
			&address,
			&profession,
			&municipalityID,
			&expeditionMunicipalityID,
			&originOfFunds,
			&economicActivity,
			&paymentForm,
			&originOfFundsDetail,
			&otherIncomeSource,
			&otherIncomeDetail,
			&businessAddress,
			&businessMunicipalityID,
			&countryID,
		)
		if err != nil {
			return nil, fmt.Errorf("no se pudo interpretar un cliente cargado: %w", err)
		}
		clients[id] = &models.ClientData{
			ID:                       id,
			PEP:                      pep.String,
			DocumentType:             documentType.String,
			FirstName:                firstName.String,
			LastName:                 lastName.String,
			BirthDate:                birthDate.String,
			DUIExpeditionDate:        expeditionDate.String,
			DUIRenewalDate:           renewalDate.String,
			Address:                  address.String,
			Profession:               profession.String,
			MunicipalityID:           municipalityID.Int64,
			ExpeditionMunicipalityID: expeditionMunicipalityID.Int64,
			OriginOfFunds:            originOfFunds.String,
			EconomicActivity:         economicActivity.String,
			PaymentForm:              paymentForm.String,
			OriginOfFundsDetail:      originOfFundsDetail.String,
			OtherIncomeSource:        otherIncomeSource.String,
			OtherIncomeDetail:        otherIncomeDetail.String,
			BusinessAddress:          businessAddress.String,
			BusinessMunicipalityID:   businessMunicipalityID.Int64,
			CountryID:                countryID.Int64,
			CountryKnown:             countryID.Valid,
			References:               make(map[string]models.Reference),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("no se pudo completar la carga de clientes: %w", err)
	}
	return clients, nil
}

func (l *Loader) loadComplements(ctx context.Context, ids []int64, clients map[int64]*models.ClientData) error {
	if !l.tables.Has(database.TableComplement) {
		return nil
	}
	query := `
SELECT
    c.id_cliente,
    c.fecha_creacion,
    c.fecha_inicio_negocio,
    c.ingresos,
    c.egresos,
    c.monto_ingreso,
    c.local_negocio,
    c.antecedentes
FROM scc_creditos_clientes_complemento c
INNER JOIN (
    SELECT id_cliente, MAX(id_cliente_negocio) AS max_id
    FROM scc_creditos_clientes_complemento
    WHERE id_cliente IN (` + placeholders(len(ids)) + `)
    GROUP BY id_cliente
) latest ON latest.max_id = c.id_cliente_negocio
WHERE c.id_cliente IN (` + placeholders(len(ids)) + `)`

	args := append(int64Args(ids), int64Args(ids)...)
	rows, err := l.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("no se pudieron cargar complementos del cliente: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id                            int64
			createdAt, businessStart      sql.NullString
			dailySales, dailyCosts        sql.NullFloat64
			monthlyIncome                 sql.NullFloat64
			businessLocal, judicialRecord sql.NullString
		)
		if err := rows.Scan(
			&id,
			&createdAt,
			&businessStart,
			&dailySales,
			&dailyCosts,
			&monthlyIncome,
			&businessLocal,
			&judicialRecord,
		); err != nil {
			return fmt.Errorf("no se pudo interpretar un complemento cargado: %w", err)
		}
		client, ok := clients[id]
		if !ok {
			continue
		}
		client.HasComplement = true
		client.ComplementCreatedAt = createdAt.String
		client.BusinessStartDate = businessStart.String
		client.DailySales = dailySales.Float64
		client.DailyCosts = dailyCosts.Float64
		client.MonthlyIncome = monthlyIncome.Float64
		client.BusinessLocal = businessLocal.String
		client.JudicialRecord = judicialRecord.String
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("no se pudo completar la carga de complementos: %w", err)
	}
	return nil
}

func (l *Loader) loadReferences(ctx context.Context, ids []int64, clients map[int64]*models.ClientData) error {
	if !l.tables.Has(database.TableReferences) {
		return nil
	}
	query := `
SELECT
    id_cliente,
    tipo_referencia,
    nombre_referencia,
    parentesco,
    n_cuenta,
    telefono_trabajo,
    fecha_ingreso_info_ref
FROM scc_creditos_clientes_referencias
WHERE id_cliente IN (` + placeholders(len(ids)) + `)
ORDER BY id_cliente, id_cliente_referencia ASC`

	rows, err := l.db.QueryContext(ctx, query, int64Args(ids)...)
	if err != nil {
		return fmt.Errorf("no se pudieron cargar referencias del cliente: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id                                         int64
			referenceType, name, relationship          sql.NullString
			accountNumber, workPhone, informationSince sql.NullString
		)
		if err := rows.Scan(
			&id,
			&referenceType,
			&name,
			&relationship,
			&accountNumber,
			&workPhone,
			&informationSince,
		); err != nil {
			return fmt.Errorf("no se pudo interpretar una referencia cargada: %w", err)
		}
		client, ok := clients[id]
		if !ok {
			continue
		}
		// The PHP loop assigns each type, so the last row wins.
		client.References[referenceType.String] = models.Reference{
			Type:             referenceType.String,
			Name:             name.String,
			Relationship:     relationship.String,
			AccountNumber:    accountNumber.String,
			WorkPhone:        workPhone.String,
			InformationSince: informationSince.String,
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("no se pudo completar la carga de referencias: %w", err)
	}
	return nil
}

func (l *Loader) loadImages(ctx context.Context, ids []int64, clients map[int64]*models.ClientData) error {
	if !l.tables.Has(database.TableClientImages) {
		return nil
	}
	query := `
SELECT id_cliente, COUNT(*) AS total, MAX(fecha_creacion) AS ultima
FROM scc_creditos_imgs_clientes
WHERE id_cliente IN (` + placeholders(len(ids)) + `)
GROUP BY id_cliente`

	rows, err := l.db.QueryContext(ctx, query, int64Args(ids)...)
	if err != nil {
		return fmt.Errorf("no se pudieron cargar imágenes del cliente: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id     int64
			total  sql.NullInt64
			lastAt sql.NullString
		)
		if err := rows.Scan(&id, &total, &lastAt); err != nil {
			return fmt.Errorf("no se pudo interpretar el resumen de imágenes: %w", err)
		}
		if client, ok := clients[id]; ok {
			client.ImageCount = total.Int64
			client.LastImageAt = lastAt.String
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("no se pudo completar la carga de imágenes: %w", err)
	}
	return nil
}

func (l *Loader) loadContracts(ctx context.Context, ids []int64, clients map[int64]*models.ClientData, table, origin string) error {
	query := "SELECT id_cliente, fecha FROM " + table +
		" WHERE id_cliente IN (" + placeholders(len(ids)) + ")"

	rows, err := l.db.QueryContext(ctx, query, int64Args(ids)...)
	if err != nil {
		return fmt.Errorf("no se pudieron cargar contratos (%s): %w", origin, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id   int64
			date sql.NullString
		)
		if err := rows.Scan(&id, &date); err != nil {
			return fmt.Errorf("no se pudo interpretar un contrato (%s): %w", origin, err)
		}
		if client, ok := clients[id]; ok {
			client.Contracts = append(client.Contracts, models.Contract{
				Date:   date.String,
				Origin: origin,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("no se pudo completar la carga de contratos (%s): %w", origin, err)
	}
	return nil
}

func (l *Loader) loadPayments(ctx context.Context, ids []int64, clients map[int64]*models.ClientData, table, amountColumn string) error {
	query := `
SELECT c.id_cliente, a.fecha, ` + amountColumn + `, a.efectivo, a.cheque, a.otro
FROM ` + table + ` a
INNER JOIN scc_creditos_contratos` + hpJoinSuffix(table) + ` c ON c.id_contrato = a.id_contrato
WHERE c.id_cliente IN (` + placeholders(len(ids)) + `)`

	rows, err := l.db.QueryContext(ctx, query, int64Args(ids)...)
	if err != nil {
		return fmt.Errorf("no se pudieron cargar abonos (%s): %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id                 int64
			date               sql.NullString
			amount             sql.NullFloat64
			cash, check, other sql.NullString
		)
		if err := rows.Scan(&id, &date, &amount, &cash, &check, &other); err != nil {
			return fmt.Errorf("no se pudo interpretar un abono (%s): %w", table, err)
		}
		if client, ok := clients[id]; ok {
			client.Payments = append(client.Payments, models.Payment{
				Date:   date.String,
				Amount: amount.Float64,
				Cash:   cash.String,
				Check:  check.String,
				Other:  other.String,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("no se pudo completar la carga de abonos (%s): %w", table, err)
	}
	return nil
}

func hpJoinSuffix(paymentTable string) string {
	if paymentTable == database.TablePaymentsHP {
		return "_hp"
	}
	return ""
}

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

func int64Args(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}
