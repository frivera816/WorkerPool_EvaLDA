package database

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/go-sql-driver/mysql"

	"worker_pool_eva/internal/config"
)

const (
	TableClients          = "scc_creditos_clientes"
	TableJuramento        = "scc_creditos_clientes_juramento"
	TableComplement       = "scc_creditos_clientes_complemento"
	TableReferences       = "scc_creditos_clientes_referencias"
	TableClientImages     = "scc_creditos_imgs_clientes"
	TableContractsMarkets = "scc_creditos_contratos_mercados"
	TableContractsHP      = "scc_creditos_contratos_hp"
	TableContractsStock   = "scc_creditos_contratos_inventarios"
	TableContracts        = "scc_creditos_contratos"
	TablePayments         = "scc_creditos_abonos"
	TablePaymentsHP       = "scc_creditos_abonos_hp"
	TableEvaluations      = "scc_creditos_evaluacion_riesgo_cliente"
	TableMunicipalities   = "emp_municipios"
	TableDepartments      = "emp_departamentos"
	TableCountries        = "emp_paises"
	enterpriseSchema      = "empresas"
)

var currentSchemaTables = []string{
	TableClients,
	TableJuramento,
	TableComplement,
	TableReferences,
	TableClientImages,
	TableContractsMarkets,
	TableContractsHP,
	TableContractsStock,
	TableContracts,
	TablePayments,
	TablePaymentsHP,
	TableEvaluations,
}

var enterpriseTables = []string{
	TableMunicipalities,
	TableDepartments,
	TableCountries,
}

type DB struct {
	connection *sql.DB
	queries    atomic.Int64
}

type TableAvailability struct {
	present map[string]bool
}

func (t TableAvailability) Has(table string) bool {
	return t.present[table]
}

func (t TableAvailability) ContractsAvailable() bool {
	return t.Has(TableContractsMarkets) && t.Has(TableContractsHP)
}

func (t TableAvailability) GeoAvailable() bool {
	return t.Has(TableMunicipalities) && t.Has(TableDepartments) && t.Has(TableCountries)
}

func (t TableAvailability) MissingCoreTables() []string {
	var missing []string
	for _, table := range []string{TableClients, TableEvaluations} {
		if !t.Has(table) {
			missing = append(missing, table)
		}
	}
	return missing
}

func Open(ctx context.Context, cfg config.Config) (*DB, error) {
	driverConfig := mysql.NewConfig()
	driverConfig.User = cfg.DBUsername
	driverConfig.Passwd = cfg.DBPassword
	driverConfig.Net = "tcp"
	driverConfig.Addr = net.JoinHostPort(cfg.DBHost, strconv.Itoa(cfg.DBPort))
	driverConfig.DBName = cfg.DBDatabase
	driverConfig.Params = map[string]string{"charset": "utf8mb4"}
	driverConfig.Timeout = 5 * time.Second
	driverConfig.ReadTimeout = cfg.QueryTimeout
	driverConfig.WriteTimeout = cfg.QueryTimeout

	connection, err := sql.Open("mysql", driverConfig.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("no se pudo configurar la conexión a MariaDB; verifique la configuración")
	}
	connection.SetMaxOpenConns(4)
	connection.SetMaxIdleConns(2)
	connection.SetConnMaxLifetime(5 * time.Minute)

	db := &DB{connection: connection}
	if err := db.Ping(ctx); err != nil {
		connection.Close()
		return nil, fmt.Errorf("no se pudo conectar a MariaDB; verifique el archivo .env y los permisos de solo lectura")
	}
	return db, nil
}

func (db *DB) Ping(ctx context.Context) error {
	return db.connection.PingContext(ctx)
}

func (db *DB) Close() error {
	return db.connection.Close()
}

func (db *DB) QueryCount() int64 {
	return db.queries.Load()
}

func (db *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	db.queries.Add(1)
	return db.connection.QueryContext(ctx, query, args...)
}

func (db *DB) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return db.connection.BeginTx(ctx, nil)
}

func (db *DB) DetectTables(ctx context.Context) (TableAvailability, error) {
	currentPlaceholders := placeholders(len(currentSchemaTables))
	enterprisePlaceholders := placeholders(len(enterpriseTables))
	query := "SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE " +
		"(TABLE_SCHEMA = DATABASE() AND TABLE_NAME IN (" + currentPlaceholders + ")) OR " +
		"(TABLE_SCHEMA = ? AND TABLE_NAME IN (" + enterprisePlaceholders + "))"

	args := make([]any, 0, len(currentSchemaTables)+len(enterpriseTables)+1)
	for _, table := range currentSchemaTables {
		args = append(args, table)
	}
	args = append(args, enterpriseSchema)
	for _, table := range enterpriseTables {
		args = append(args, table)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return TableAvailability{}, fmt.Errorf("no se pudo detectar la disponibilidad de tablas: %w", err)
	}
	defer rows.Close()

	present := make(map[string]bool)
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return TableAvailability{}, fmt.Errorf("no se pudo leer la disponibilidad de tablas: %w", err)
		}
		present[table] = true
	}
	if err := rows.Err(); err != nil {
		return TableAvailability{}, fmt.Errorf("no se pudo completar la detección de tablas: %w", err)
	}
	return TableAvailability{present: present}, nil
}

func (db *DB) SelectClientsWithoutEvaluation(ctx context.Context, limit int) ([]int64, error) {
	if limit < 0 {
		return nil, fmt.Errorf("el límite de clientes no puede ser negativo")
	}
	query := `
SELECT c.id_cliente
FROM scc_creditos_clientes c
WHERE NOT EXISTS (
    SELECT 1
    FROM scc_creditos_evaluacion_riesgo_cliente e
    WHERE e.id_cliente = c.id_cliente
)
ORDER BY c.id_cliente`
	if limit > 0 {
		query += "\nLIMIT ?"
	}

	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = db.QueryContext(ctx, query, limit)
	} else {
		rows, err = db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, fmt.Errorf("no se pudieron seleccionar clientes sin evaluación: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0)
	if limit > 0 {
		ids = make([]int64, 0, limit)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("no se pudo leer un cliente seleccionado: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("no se pudo completar la selección de clientes: %w", err)
	}
	return ids, nil
}

func placeholders(count int) string {
	if count < 1 {
		return ""
	}
	result := "?"
	for i := 1; i < count; i++ {
		result += ",?"
	}
	return result
}
