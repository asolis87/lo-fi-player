# lo-fi-player

> Reproductor de música lo-fi de codigo limpio para licencias, nativo
> de terminal y pensado para desarrolladores.

`lo-fi-player` es una CLI/TUI de binario unico, orientada al teclado,
que reproduce audio ambiental con atribucion limpia desde un catalogo
curado. No consume streams de Spotify, YouTube ni SoundCloud; no
requiere navegador, GUI ni cuenta. Tras la primera sincronizacion, el
reproductor opera sin red.

## Estado

La cadena del slice #1 esta **embarcada**: las 11 unidades de trabajo
del plan de PRs encadenados ya fueron fusionadas. Las cadenas del
slice #2 (auditoria del seed del catalogo y bake del SHA de release)
tambien estan **embarcadas**: PR-A (seed verificado de 4 pistas CC BY
4.0), PR-B (parser de licencias y reportes de verificacion firmados),
PR-C (re-staging del `manifest.json` con las pistas reales), PR-D
(fix del loader drift: cada subdir por pista es obligatorio) y PR-E
(pipeline de release con bake de SHA y gate de verificacion).
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

- `lofi play` — abre la TUI (navegador de pistas, now-playing, cola y
  vistas de atribucion; pulsa `?` dentro para ver el mapa de teclas).
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
