# Contribuir a lo-fi-player

Gracias por tu interes en `lo-fi-player`. Esta guia es el contrato
entre cada contribuidor y cada revisor. Es corta a proposito: cada
regla aqui existe porque la cadena de cambios la forzo.

## Reglas base

1. **Una unidad de trabajo por commit.** Cada commit es un
   entregable de comportamiento, fix, migracion o unidad documental;
   no es una cubeta por tipo de archivo. Las pruebas y la
   documentacion de esa unidad viven en el mismo commit.
2. **Mantenerse dentro del presupuesto de revision de 400 lineas.**
   Las adiciones y borrados autorales de cualquier PR deben rondar
   o quedar por debajo de 400 lineas. Si el slice es mas grande,
   partirlo en una cadena de PRs antes de abrir el request.
3. **Seguir la estrategia de cadena.** `lo-fi-player` usa una
   **cadena feature-branch**: una rama trackera (`feat/lo-fi-player`)
   agrega el slice y cada PR hijo apunta contra la rama del PR
   previo inmediato. Los PRs hijos nunca apuntan contra `main`
   directamente. La rama trackera `feat/lo-fi-player` esta
   reservada: nunca se usa como destino de un PR hijo individual.
4. **Deferir el detalle a la spec.** Cuando tengas duda, lee
   `openspec/specs/<capacidad>/spec.md` (las specs delta viven bajo
   `openspec/specs/` y los snapshots historicos bajo
   `openspec/changes/archive/`). La spec es el criterio de
   aceptacion; un PR que la contradiga debe justificarlo en la
   descripcion.
5. **Sin trailers de co-autoria IA.** Los mensajes de commit deben
   seguir el formato de Conventional Commits de abajo. No incluyas
   `Co-Authored-By:` ni ninguna atribucion de asistencia IA.

## Nombres de rama

Los nombres de rama codifican el numero de PR y el area de trabajo.
El formato es:

```text
<type>/pr-<numero>-<short-kebab-slug>
```

| Segmento | Regla | Ejemplo |
| --- | --- | --- |
| `<type>` | Uno de `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `ci`, `build`, `perf`. | `feat`, `chore` |
| `pr-<numero>` | Numero de PR con cero a la izquierda del plan encadenado. PR #0 es el bootstrap. | `pr-0`, `pr-1` |
| `<short-kebab-slug>` | Resumen en lower-kebab del slice (<= 4 palabras). | `pr-0-bootstrap`, `mpv-ipc-adapter` |

Ejemplos trabajados:

- `feat/pr-0-bootstrap` — bootstrap del repositorio.
- `feat/pr-1-go-skeleton` — modulo Go + esqueleto `cmd/lofi` +
  puerto `AudioBackend`.
- `feat/pr-5-catalog-loader` — loader de catalogo + verificacion de
  checksum.
- `fix/pr-7-credits-prefix` — fix focal dentro del slice de PR #7.
- `feat/pr-A-licit-catalog` — slice #2, PR-A (seed verificado).

La rama trackera `feat/lo-fi-player` esta reservada y nunca se usa
como destino de un PR hijo.

## Mensajes de commit — Conventional Commits

Cada mensaje de commit **debe** seguir la especificacion
[Conventional Commits 1.0.0](https://www.conventionalcommits.org/).
El header es el contrato; el body explica el *por que* y el hand-off
al revisor.

### Formato

```text
<type>(<scope-opcional>): <resumen imperativo>

<body — que y por que, no que archivos cambiaron>

<footer — referencias, breaking changes, reviewers>
```

### Valores de `<type>` permitidos

| Type | Cuando usarlo |
| --- | --- |
| `feat` | Comportamiento nuevo visible para el usuario: comando, vista o API. |
| `fix` | Bug fix que cambia comportamiento visible al usuario. |
| `refactor` | Reestructuracion interna sin cambio visible al usuario. |
| `perf` | Cambio que mejora una metrica medible de performance. |
| `test` | Agregar o arreglar pruebas sin cambio de produccion. |
| `docs` | Cambio solo de documentacion (README, CONTRIBUTING, design notes). |
| `chore` | Tooling, metadata del repo, glue de build. Sin codigo de produccion. |
| `ci` | Workflows de CI y cambios solo-CI. |
| `build` | Sistema de build, pinning de dependencias, bumps de version. |
| `revert` | Revertir un commit previo; el body debe referenciar el SHA. |

### `<scope>` opcional

El scope es un sustantivo corto que nombra el area del codebase.
Usa scopes lowercase de un solo token que coincidan con los nombres
de modulos planeados. Ejemplos: `audio`, `catalog`, `config`, `tui`,
`cli`, `credits`, `sync`, `bootstrap`.

### El body

El body responde tres preguntas del revisor:

1. **Que** entrego este commit en una oracion?
2. **Por que** fue necesario (link al escenario de la spec, decision
   de diseno, o riesgo que cierra)?
3. **Cual es el limite de rollback** (que archivos / comportamiento
   se pueden revertir sin remover trabajo no relacionado)?

### El footer

Usa el footer para:

- `Refs:` referencias a IDs de spec (por ejemplo `Refs: REQ-CLI-1,
  S-CLI-1`).
- `Closes:` referencias a GitHub issues.
- Parrafos `BREAKING CHANGE:` cuando el commit fuerce una migracion.

**No** agregues trailers `Co-Authored-By:`, incluyendo los generados
por herramientas de IA.

### Ejemplos trabajados

```text
feat(audio): add AudioBackend port and mock for the lo-fi-player

Introduce the audio.Backend interface that decouples the TUI and
the CLI dispatcher from concrete adapters. The interface mirrors
the surface in spec #287 (Load/Play/Pause/Stop/SetVolume/Seek/State/
Close) and ships with a deterministic mock so downstream slices
write tests against the port, not against mpv.

Rollback: deleting internal/audio/port.go and internal/audio/mock.go
leaves the upstream modules untouched.

Refs: REQ-AUD-1
```

```text
fix(catalog): reject floating refs in the first-run fetch URL

The SHA-pinned fetch contract from spec #289 forbids mutable refs.
A regression had re-enabled the `latest` fallback for one path;
this commit re-tightens the loader and adds a regression test.

Refs: REQ-FCH-1
```

## Pull requests

1. **Un PR por unidad de trabajo.** Usa el plan de PRs encadenados
   en `openspec/changes/archive/<slice>/tasks.md` para localizar el
   numero y el slice del PR.
2. **Brancheo desde la rama del PR previo**, no desde `main`. La
   cadena feature-branch significa que cada PR es un delta sobre el
   anterior.
3. **El titulo del PR es el header de Conventional Commits.** El
   titulo del PR hace doble funcion como subject del commit squash;
   mantenlo bajo 72 caracteres.
4. **La descripcion del PR cita la spec.** Linkea los requirements y
   scenarios que el PR cierra. Si el PR se desvía del diseno,
   justifica la desviacion en la descripcion *antes* de pedir
   review.
5. **CI debe estar en verde.** El pipeline stub en PR #0 falla a
   proposito hasta que aterriza el modulo Go; desde PR #1 en
   adelante `go vet`, `go build` y `go test` deben pasar todos.
6. **Actualiza los artefactos spec-driven si el contrato cambia.**
   Cualquier cambio de contrato visible al usuario debe viajar con
   el delta de spec, no por detras de el.

## Builds de release manuales (PR-E)

Ademas del pipeline tag-triggered (`.github/workflows/release.yml`),
puedes producir y verificar un binario de release localmente.
Esto es util para reproduccion de incidentes, debug de un SHA
especifico, o para mantener el catalogo sin abrir un tag `v*`.

```bash
# Resolver el SHA de catalog/v1/manifest.json en el HEAD actual
MANIFEST_SHA=$(git rev-parse HEAD:catalog/v1/manifest.json)

# Compilar con el SHA horneado en internal/catalog.FirstRunCommitSHA
go build \
    -ldflags "-X github.com/asolis87/lo-fi-player/internal/catalog.FirstRunCommitSHA=$MANIFEST_SHA" \
    -o lofi-release \
    ./cmd/lofi

# Verificar: runtime SHA == SHA esperado
./scripts/verify-release.sh ./lofi-release "$MANIFEST_SHA"
```

`scripts/verify-release.sh` delega en
`lofi verify-release-binary <bin> <sha>`, asi el camino bash y las
pruebas Go unitarias comparten la misma implementacion
(`internal/catalog.VerifyBakedSHA`). El gate lee el valor runtime
del simbolo `FirstRunCommitSHA` mediante `debug/{macho,elf,pe}` y
lo compara contra el SHA esperado. Esta implementacion sustituye
al gate previo `strings <bin> | grep <PLACEHOLDER_SHA>` que estaba
roto: el linker de Go conserva el literal del codigo fuente en
rodata adyacente al valor runtime reescrito, y la busqueda por
substring marcaba como fallido a todo binario correctamente
horneado.

Codigos de salida del gate:

| Codigo | Significado |
| --- | --- |
| 0 | El SHA runtime coincide con el esperado. Release OK. |
| 1 | El binario es ilegible, lleva el literal placeholder, o el SHA runtime no coincide. Release bloqueado. |
| 2 | Error de invocacion (arity incorrecta, archivo no regular). |

## Checks locales

Antes de hacer push de una rama, corre las mismas puertas que corre
la CI:

```bash
go vet ./...
go build ./...
go test ./...
```

PR #0 es el unico PR donde estos comandos fallan con un mensaje de
"no main module"; eso es esperado y esta documentado en `README.md`.

## Codigo de conducta

Se amable, se preciso y asume buena fe. Los revisores deben explicar
el *por que* detras de cada pedido de cambio; los contribuidores
deben responder el *por que* detras de cada implementacion. El
desacuerdo es bienvenido — el silencio no.
