# lo-fi-player

> Reproductor de música lo-fi de codigo limpio para licencias, nativo
> de terminal y pensado para desarrolladores.

`lo-fi-player` es una CLI/TUI de binario unico, orientada al teclado,
que reproduce audio ambiental con atribucion limpia desde un catalogo
curado. No consume streams de Spotify, YouTube ni SoundCloud; no
requiere navegador, GUI ni cuenta. Tras la primera sincronizacion, el
reproductor opera sin red.

## Estado

Las cadenas de los slices 1–4 estan **embarcadas** en la rama tracker
`feat/lo-fi-player`:

- **Slice #1** — las 11 unidades de trabajo del plan encadenado ya
  fueron fusionadas (esqueleto CLI/TUI + puerto `AudioBackend` +
  adaptadores mpv/oto + fallback procedural + CLI + TUI Bubble Tea).
- **Slice #2** — auditoria del seed del catalogo y bake del SHA de
  release: PR-A (seed verificado de 4 pistas CC BY 4.0), PR-B
  (parser de licencias y reportes de verificacion firmados), PR-C
  (re-staging del `manifest.json` con las pistas reales), PR-D
  (fix del loader drift: cada subdir por pista es obligatorio) y
  PR-E (pipeline de release con bake de SHA y gate de
  verificacion).
- **Slice #3** — ciclo de vida persistente de reproduccion: estado
  v2 + historial MRU, hidratacion al arrancar, prompt de
  reanudacion TTY/non-TTY, persistencia de identidad al
  cargar/reproducir y guardado en `q` / `Ctrl+C` + SIGINT/SIGTERM.
- **Slice #4** — TUI real sobre backend + eventos + fallback:
  contrato `audio.EventSource`, correlacion `start-file`/`end-file`
  por generacion, identidades `Selected`/`Loaded`/`Playing`,
  auto-avance circular en `EventEnd` valido, fallback a
  `procedural:rain` ante `EventError` y cierre exacto-una-vez del
  backend.

CI en verde, binario compila y las pruebas pasan.

| Area | Estado |
| --- | --- |
| Metadata del repo + CI | embarcado (PR #0) |
| Modulo Go + esqueleto `cmd/lofi` + puerto `AudioBackend` | embarcado (PR #1) |
| Store TOML atomico con conciencia XDG | embarcado (PR #2) |
| Adaptador mpv JSON-IPC (backend principal) | embarcado (PR #3) |
| Fallback procedural (`procedural:rain`) + sink `oto/v3` | embarcado (PR #4, #14) |
| Loader de catalogo + verificacion SHA-256 en streaming | embarcado (PR #5) |
| Fetch de primer arranque + sync transaccional | embarcado (PR #6) |
| CLI `lofi credits` (NOTICE + `--json`) | embarcado (PR #7) |
| Vistas TUI (Bubble Tea) cableadas a `lofi play` | embarcado (PR #8) |
| Dispatch CLI + `lofi play <id-or-station>` headless | embarcado (PR #9) |
| Seed `catalog/v1/` + `LICENSE` (MIT) | embarcado (PR #10) |
| Seed verificado: 4 pistas CC BY 4.0 con reportes firmados | embarcado (PR-A, PR-C) |
| Parser de licencias CC0/CC-BY/CC-BY-SA y reportes de verificacion | embarcado (PR-B) |
| Fix del loader drift: `audio.mp3` y `track.json` obligatorios | embarcado (PR-D) |
| Pipeline de release tag-triggered con bake de SHA + `verify-release-binary` | embarcado (PR-E) |
| **Slice 3 — ciclo de vida persistente de reproduccion** | |
| Schema v2 del store TOML + migracion desde v1 | embarcado (PR #25) |
| `PlaybackState` (volumen + historial MRU, cap 25) con persistencia atomica | embarcado (PR #26) |
| Hidratacion del estado al arrancar y guia `lofi sync` cuando no hay catalogo | embarcado (PR #27) |
| Prompt de reanudacion TTY + auto-hidratacion non-TTY y filtrado del historial contra el catalogo | embarcado (PR #28) |
| Persistencia de identidad al cargar/reproducir + guardado en `q`/`Ctrl+C` y en SIGINT/SIGTERM (5 s timeout) | embarcado (PR #29) |
| Fix de wiring publico: limites del estado persistente en la sesion interactiva | embarcado (PR #30) |
| **Slice 4 — TUI real sobre backend + eventos + fallback** | |
| Fix de ruta del socket mpv en macOS (limite de 104 bytes de `sun_path`) | embarcado (PR #32) |
| Contrato `audio.EventSource` con canal FIFO cerrable y generacion por carga | embarcado (PR #34) |
| Correlacion `start-file`/`end-file` por `playlist_entry_id` en el adaptador mpv | embarcado (PR #36) |
| TUI: identidades `Selected` / `Loaded` / `Playing` y transicion Load-Play-Persist protegida por generacion | embarcado (PR #38) |
| Composicion interactiva: `audio.Select` con `WithMpvProbe` + `WithMpvFactory` y `Close` exacto-una-vez via `sync.Once` | embarcado (PR #40) |
| Consumo de eventos + auto-avance circular y fallback a `procedural:rain` en runtime ante `EventError` | embarcado (PR #42) |

El catalogo `catalog/v1/` que se embarca con el binario esta
**verificado**: cuatro pistas CC BY 4.0 (LOFI LION y Lee Rosevere)
con su `LICENSE.txt`, su `track.json` y su reporte de verificacion
firmado bajo `catalog/v1/verification/`. El binario publicado hornea
(`-ldflags`) el SHA de `catalog/v1/manifest.json` en
`internal/catalog.FirstRunCommitSHA`; `lofi sync` rechaza ese SHA
placeholder si el binario se compilo sin `-ldflags`.

## Por que existe

Las personas developer que viven en la terminal quieren musica
ambiental que no rompa el foco del teclado ni fuerce un navegador.
Los reproductores con GUI exigen cambios de contexto; los wrappers de
CLI para YouTube/Spotify son fragiles y quedan legalmente expuestos;
los scripts de shell sobre `mpv` o `cmus` carecen de descubrimiento,
cola y atribucion. `lo-fi-player` ocupa ese hueco con un reproductor
nativo de terminal que embarca su propio catalogo, expone la
atribucion inline y se queda dentro del flujo keyboard-first.

## Subcomandos

```
lofi play [track|procedural:station]
lofi list
lofi credits [--json]
lofi sync
lofi verify-release-binary <ruta-binario> <sha-esperado>
```

- `lofi play` — abre la TUI sobre el backend real. La composicion
  invoca `audio.Select` con un probe de `mpv` (`exec.LookPath`) y un
  factory privado; si no hay backend disponible imprime un mensaje
  accionable en stderr y sale con codigo 1 sin levantar Bubble Tea.
  La sesion interactiva carga el `PlaybackState` persistido, lanza
  el prompt de reanudacion en TTY (auto-hidrata en non-TTY) y
  mantiene tres identidades separadas: `SelectedIdx` (intencion),
  `LoadedID` + `LoadedGeneration` (lo que el backend acepto en el
  ultimo Load exitoso) y `PlayingID` (lo que confirmo Play). El
  contrato `audio.EventSource` entrega `EventEnd` correlacionado por
  generacion y `EventError`: un `EventEnd` cuya generacion coincide
  con `LoadedGeneration` dispara auto-avance circular al siguiente
  indice; uno con generacion obsoleta se descarta sin navegar, Load,
  Play ni persistir. Un `EventError` actualiza el banner y, si el
  seam `RebindBackend` esta cableado, intercambia el backend por
  `procedural:rain` sin perder la obligacion de cerrar el backend
  viejo. La sesion interactiva protege el cierre exacto-una-vez via
  `sync.Once`, asi la TUI entrega el sink de audio al SO sin
  doble-close. Pulsa `?`
  dentro para ver el mapa de teclas.
- `lofi play <track-id>` — reproduce una pista en modo headless y
  sale cuando termina.
- `lofi play procedural:rain` — reproduce la estacion de lluvia
  ambiental en headless. El backend procedural genera muestras en un
  ring buffer del lado Go que un goroutine de pump drena hacia el
  dispositivo de audio del sistema via `oto/v3` (ALSA en Linux,
  CoreAudio en macOS). Es el fallback offline: no necesita `mpv`,
  ni archivo de audio, ni red, pero si requiere un dispositivo de
  audio funcional en el host.
- `lofi list` — imprime el catalogo local como tabla (o `--json` para
  el `manifest.json` crudo).
- `lofi credits` — imprime el bloque NOTICE de atribucion por pista,
  o `--json` para los mismos datos estructurados.
- `lofi sync` — descarga el manifest con SHA pinned desde el origen
  del catalogo y lo aplica atomicamente en el store local.
- `lofi verify-release-binary <ruta> <sha>` — gate del pipeline de
  release (PR-E). Lee el valor runtime del simbolo
  `FirstRunCommitSHA` mediante `debug/{macho,elf,pe}` y lo compara
  contra el SHA esperado. Sustituye al gate previo
  `strings | grep <PLACEHOLDER_SHA>` que estaba estructuralmente
  roto. Sale 0 cuando coinciden, 1 cuando hay mismatch, 2 cuando la
  invocacion es invalida.

Los subcomandos desconocidos y `lofi --help` imprimen el bloque de
uso de arriba y salen con codigo 2.

## Arranque rapido

```bash
go build -o lofi ./cmd/lofi
./lofi sync      # unica vez: descarga el catalogo con SHA pinned
./lofi play      # abre la TUI
./lofi list      # o navega el catalogo desde el shell
./lofi credits   # o inspecciona la atribucion desde el shell
```

Si `mpv` no esta instalado, `lofi play procedural:rain` sigue
funcionando a traves del backend procedural (no pasa por
`audio.Select`, asi que la ausencia de `mpv` es irrelevante). En
Linux el build necesita `libasound2-dev` y `pkg-config` para el
binding cgo de `oto/v3` (ver `.github/workflows/ci.yml`); en un host
sin dispositivo de audio funcional el backend procedural cae a un
sink no-op para que el binario no truene.

### Build de release con SHA bake (manual)

Para producir un binario verificable sin disparar el pipeline de
GitHub Actions (mantenimiento, reproduccion local, debugging):

```bash
MANIFEST_SHA=$(git rev-parse HEAD:catalog/v1/manifest.json)
go build -ldflags "-X github.com/asolis87/lo-fi-player/internal/catalog.FirstRunCommitSHA=$MANIFEST_SHA" \
    -o lofi-release ./cmd/lofi
./scripts/verify-release.sh ./lofi-release "$MANIFEST_SHA"
```

`scripts/verify-release.sh` delega en
`go run ./cmd/lofi verify-release-binary <bin> <sha>`, asi el camino
bash y las pruebas Go unitarias comparten la misma implementacion
del gate. Exit 0 significa "runtime SHA == SHA esperado"; exit 1
significa "placeholder, mismatch o binario no legible"; exit 2 es
error de invocacion.

## Disposicion del repositorio

```text
cmd/lofi/           composition root, dispatcher CLI, wiring de subcomandos
internal/tui/       modelo Bubble Tea, cuatro vistas, mapa de teclas
internal/audio/     puerto AudioBackend + adaptador mpv + fallback procedural
internal/catalog/   schema de pista, LoadFromDir, verify SHA-256, Syncer, release gate
internal/config/    store TOML atomico con conciencia XDG y recuperacion
internal/credits/   renderizador del bloque NOTICE (texto + --json)
internal/license/   parser por pista y reportes de verificacion firmados
internal/skeleton/  helpers de composicion usados por el dispatcher
catalog/v1/         arbol versionado de pistas, license-clean (4 pistas CC BY 4.0)
catalog/v1/verification/   reportes firmados y snapshots HTML de licencias
.github/workflows/  pipeline CI (go vet, go build, go test) + release.yml tag-triggered
LICENSE             MIT
openspec/           specs de capacidades delta bajo openspec/specs/
scripts/verify-release.sh  gate bash del SHA bake (delega en el subcomando Go)
```

## Contribuir

Lee [`CONTRIBUTING.md`](./CONTRIBUTING.md) antes de abrir un pull
request. Cubre la convencion de nombres de rama, el formato de
Conventional Commits, la regla de commit por unidad de trabajo y la
estrategia de cadena de PRs.

## Licencia

El repositorio esta bajo **MIT**. Ver [`LICENSE`](./LICENSE). Las
licencias por pista del catalogo embarcado se exponen via
`lofi credits` y la vista de atribucion de la TUI y permanecen
independientes de la licencia del repositorio.

## Siguiente etapa

Esta seccion separa la **deuda tecnica** conocida del producto
actual de las **mejoras candidatas** que aun no tienen fecha ni
diseno cerrado. Ningun item de aqui pertenece a un slice #5
empezado: cada uno tendra que pasar por una propuesta SDD antes de
tocar codigo o documentacion.

### Deuda tecnica

- **Race en `oto/v3` bajo `-race` en macOS.** Las pruebas que crean
  el dispositivo procedural pueden exponer una condicion dentro del
  driver CoreAudio de `oto/v3`. Las suites enfocadas excluyen esos
  casos conocidos mientras mantienen cobertura de carrera sobre el
  resto del flujo.
- **Falta un test directo** que verifique que `backend.Close()`
  desbloquea al listener de eventos de la TUI. `ProceduralBackend.Close`
  ya cierra el canal de eventos, pero falta probar de manera focal
  que ese cierre libera un Cmd bloqueado. Ese test documentaria el
  contrato de apagado para futuros adaptadores.

### Trabajo de producto pendiente (sin priorizar)

Candidatos mencionados por contribuidores o visibles en el codigo,
**sin diseno cerrado** y sin compromiso de fecha:

- Cola editable (reordenar / quitar pistas desde la TUI).
- Busqueda y filtro por texto sobre el catalogo y la cola.
- Seek y volumen con feedback visual: los puertos `Seek(int)` y
  `SetVolume(int)` ya viven en `audio.AudioBackend`; solo falta la
  capa de UI.
- Favoritos separados del historial MRU.
- Distribucion (Homebrew tap, paquetes `.deb` / `.rpm`, releases
  firmados fuera del pipeline tag-triggered actual).

## Reconocimientos

- Internet Archive — fuente de las 4 pistas CC BY 4.0
  (`lofi-lion-tame-the-beast` y tres pistas de Lee Rosevere). Cada
  reporte firmado en `catalog/v1/verification/<id>.md` incluye el
  snapshot HTML de la pagina de origen.
- `mpv` — backend de audio JSON-IPC para pistas del catalogo.
- `oto/v3` — sink de salida de audio via cgo para el fallback
  procedural. Requiere headers de desarrollo de ALSA al compilar en
  Linux (ver `.github/workflows/ci.yml`).
- Bubble Tea, Lip Gloss y el ecosistema Charmbracelet — TUI.
