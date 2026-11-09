# Worker Pool EVA — evaluación LDA/FT/FPADM

Worker Pool en Go para evaluar clientes de SCC. Conserva el modo benchmark sin
persistencia y añade un modo piloto transaccional limitado a cinco evaluaciones.
La selección y las reglas se basan en:

`/var/www/html/fj.rivera/modules/clientes/main/evaluacionRiesgoLDAAuto.php`

## Alcance

- Selecciona únicamente clientes sin registro en
  `scc_creditos_evaluacion_riesgo_cliente`.
- Carga datos con consultas agrupadas por lotes de `id_cliente`.
- Evalúa las 17 variables en memoria.
- Distingue puntuaciones automáticas de valores predeterminados.
- Mide carga, evaluación, consultas, velocidad y memoria.
- Por defecto (`DRY_RUN=true`) no ejecuta escrituras ni cambios de esquema.
- Con `DRY_RUN=false` ejecuta exclusivamente cinco evaluaciones nuevas en una
  transacción.
- No guarda evaluaciones ni resultados en archivos.

## Requisitos

- Go 1.23 o posterior.
- Acceso de lectura a la base MariaDB de desarrollo.
- Para el piloto, autorización expresa para insertar cinco registros.
- Credenciales completadas manualmente en `.env`.

## Configuración

El archivo `.env` incluido tiene las credenciales vacías:

```text
DB_HOST=
DB_PORT=
DB_DATABASE=
DB_USERNAME=
DB_PASSWORD=
WORKER_COUNT=5
CLIENT_LIMIT=500
BATCH_SIZE=100
DB_QUERY_TIMEOUT=30s
DRY_RUN=true
```

Complete solo los valores de conexión. `.env` está incluido en `.gitignore`.
Las variables del proceso tienen prioridad sobre el archivo.

`DB_PORT` vacío usa `3306`. Los parámetros del Worker Pool tienen los valores
iniciales mostrados arriba y pueden modificarse sin recompilar.

`CLIENT_LIMIT=0` procesa **todos** los clientes sin evaluación (sin límite).
Con `DRY_RUN=false`, `CLIENT_LIMIT` debe ser exactamente `5`.

`DRY_RUN` controla la persistencia:

- `DRY_RUN=true` (valor seguro por defecto): solo evalúa; nunca escribe en
  MariaDB. Permite medir la velocidad de evaluación sin `INSERT`.
- `DRY_RUN=false`: habilita el guardado del lote piloto. Exige
  `CLIENT_LIMIT=5` y que se seleccionen exactamente cinco clientes.

## Compilación

Desde la raíz del proyecto:

```powershell
go build -o bin/worker.exe ./cmd/worker
```

También puede ejecutarse directamente:

```powershell
go run ./cmd/worker
```

Para usar otro archivo de configuración:

```powershell
go run ./cmd/worker -env RUTA_AL_ARCHIVO
```

## Pruebas

```powershell
go test ./...
go vet ./...
```

Las pruebas cubren límites de fechas, valores predeterminados y su distinción,
rangos de pago, frecuencia, perfil, nacionalidad, contratos, parser de `.env` y
procesamiento concurrente con fallos individuales.

En un entorno con compilador C disponible puede añadirse:

```powershell
go test -race ./...
```

## Persistencia

El estado se controla desde `.env`:

- `DRY_RUN=true` (predeterminado): solo evalúa; no ejecuta INSERT/UPDATE/DELETE.
- `DRY_RUN=false`: evalúa y persiste **todos** los clientes seleccionados.

```powershell
$env:CLIENT_LIMIT = "0"       # todos los clientes sin evaluación
$env:DRY_RUN = "false"
go run ./cmd/worker
```

La persistencia ejecuta todos los INSERT en una sola transacción. Antes de
insertar, comprueba que ninguno de los clientes seleccionados tenga ya una
evaluación; si aparece un duplicado, se cancela el lote completo. No ejecuta
UPDATE ni DELETE.

El esquema no tiene restricción única sobre `id_cliente` ni campo de revisión;
la protección contra duplicados se realiza dentro de la transacción con una
lectura bloqueante. Las evaluaciones se identifican con
`usuario_evaluador = worker_pool_eva`.

Para el lote piloto de exactamente cinco clientes:

```powershell
$env:WORKER_COUNT = "5"
$env:CLIENT_LIMIT = "5"
$env:BATCH_SIZE = "100"
$env:DRY_RUN = "false"
go run ./cmd/worker
```

## Advertencia

La persistencia masiva puede insertar miles de registros. Una ejecución
repetida generará duplicados porque no existe restricción única. Use
`DRY_RUN=true` para pruebas de velocidad.

## Estructura

```text
cmd/worker/          Punto de entrada y métricas
internal/config/     Lectura y validación de configuración
internal/database/   Conexión, detección de tablas y selección
internal/loader/     Carga agrupada por lotes
internal/evaluator/  Reglas LDA/FT/FPADM en memoria
internal/persistence/ Persistencia transaccional del lote piloto
internal/worker/     Worker Pool con canales
internal/models/     Datos y puntuaciones
```

## Consultas

Se realiza una consulta de selección con `NOT EXISTS`, una detección inicial de
tablas y, por cada lote, hasta diez consultas agrupadas: cliente/país/juramento,
complemento, referencias, imágenes, cuatro fuentes de contratos y dos fuentes de
abonos. Con `CLIENT_LIMIT=500` y `BATCH_SIZE=100` el máximo es aproximadamente
52 consultas.

## Valores predeterminados

Los valores de respaldo se aplican únicamente cuando la regla PHP no produce una
puntuación o cuando la fuente requerida no está disponible. Cada `Score`
conserva en memoria el valor, el origen y el motivo. No se persisten.

## Métricas en consola

Al finalizar se muestran clientes seleccionados/evaluados, errores, cantidad de
puntuaciones automáticas y predeterminadas, consultas, tiempos, clientes por
segundo, workers, memoria y recolecciones de basura.

No se imprimen nombres, documentos, direcciones ni otros datos personales.

## Limitaciones conocidas

- El esquema no tiene campo de revisión automática/manual.
- El esquema no tiene restricción única sobre `id_cliente`; el piloto comprueba
  duplicados con una lectura bloqueante dentro de la misma transacción.
- La versión automática reproduce las ramas del PHP corregido, incluidas sus
  particularidades vigentes: A2 y A3 se calculan también para clientes PEP, la
  marca PEP acepta `s`, `1` y `si`, y los abonos se promedian sobre los últimos
  12 meses.
