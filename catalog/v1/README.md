# catalog/v1/ — indice del catalogo embarcado

Este directorio contiene el catalogo versionado que se embarca con
cada release de `lo-fi-player`. En el slice #2 la promesa cambio:
`catalog/v1/` ya no es un seed placeholder, sino un seed **verificado**
de cuatro pistas CC BY 4.0 con sus reportes firmados y sus snapshots
de licencia. El contrato de carga vive en
`internal/catalog/loader.go` (funcion `LoadFromDir`) y en
`openspec/specs/catalog-loader/spec.md`.

## Disposicion real (slice #2)

```text
catalog/v1/
├── manifest.json                          # indice de pistas (este archivo ES el contrato)
├── README.md                              # este archivo
├── .gitkeep                               # preserva el directorio en git
├── bigger-questions/
│   ├── track.json                         # metadata por pista (REQ-CAT-2)
│   ├── LICENSE.txt                        # texto de la licencia CC BY 4.0
│   └── audio.mp3                          # bytes de audio (MP3 192 kbps)
├── going-in-circles/
│   ├── track.json
│   ├── LICENSE.txt
│   └── audio.mp3
├── it-was-like-that-when-i-got-here/
│   ├── track.json
│   ├── LICENSE.txt
│   └── audio.mp3
├── lofi-lion-tame-the-beast/
│   ├── track.json
│   ├── LICENSE.txt
│   └── audio.mp3
└── verification/
    ├── README.md                          # contrato del reporte firmado
    ├── bigger-questions.md                # reporte firmado por pista
    ├── going-in-circles.md
    ├── it-was-like-that-when-i-got-here.md
    ├── lofi-lion-tame-the-beast.md
    └── snapshots/                         # HTML cacheado de la pagina de licencia en origen
        ├── bigger-questions.html
        ├── going-in-circles.html
        ├── it-was-like-that-when-i-got-here.html
        └── lofi-lion-tame-the-beast.html
```

Las cuatro pistas del seed verificado son:

| ID | Titulo | Artista | Licencia | Checksum SHA-256 |
| --- | --- | --- | --- | --- |
| `lofi-lion-tame-the-beast` | Tame The Beast | LOFI LION | CC BY 4.0 | `401f26d7…395b` |
| `bigger-questions` | Bigger Questions | Lee Rosevere | CC BY 4.0 | `2619c8d0…0a4e` |
| `going-in-circles` | Going In Circles | Lee Rosevere | CC BY 4.0 | `0dd018b2…45c7` |
| `it-was-like-that-when-i-got-here` | It Was Like That When I Got Here | Lee Rosevere | CC BY 4.0 | `3295f45f…6afd` |

Los checksums completos viven en `manifest.json` y en
`verification/<id>.md`. Cualquier re-seed debe actualizar ambos
lugares y volver a hornear el SHA en el binario de release.

## Contrato del manifest

`manifest.json` es el unico archivo que `lofi sync` descarga desde la
URL con SHA pinned (decision #289). Es pequeno, auditable a ojo y
`internal/catalog.LoadFromFile` lo valida antes de cualquier byte de
audio. El layout por pista existe para los eventos que el manifest
predice (verificacion de checksum, render de atribucion, output NOTICE
de `lofi credits`) pero deliberadamente **NO** se une de vuelta al
manifest: el indice se mantiene byte-estable entre releases.

## Contrato de carga (LoadFromDir)

`internal/catalog.LoadFromDir(root)` recorre `root` un nivel de
profundidad y arma un `Catalog` desde cada subdirectorio inmediato
que contenga `track.json`, ordenado por id. Despues del fix de
loader drift (PR-D #3.1) las reglas son:

- Un subdirectorio sin `track.json` aborta la carga con un error que
  nombra al subdirectorio ofensor (slice #1 los saltaba en silencio
  y producia catalogos vacios cuando el cache estaba a la mitad).
- Un subdirectorio sin `audio.mp3` aborta la carga con el mismo
  formato de error. La presencia de `audio.mp3` es obligatoria para
  que `runHeadlessPlay` pueda resolver la ruta y `backend.Load`
  reciba bytes.
- Una pista que falla `Track.Validate()` aborta la carga nombrando
  el id ofensor: embarcar un catalogo parcial es peor que fallar
  fuerte.
- Entradas no-directorio al nivel raiz (README.md, LICENSE.txt,
  verification/, snapshots/) se ignoran sin error.

El SHA-256 de cada pista se verifica en streaming durante el fetch
(`internal/catalog.Verify`); un mismatch se rechaza con el sentinel
`ErrChecksumMismatch` antes de tocar el cache local.

## Reportes de verificacion

`catalog/v1/verification/<id>.md` es el reporte firmado que promueve
una pista de `license_status: "NEEDS CONFIRMATION"` a
`"VERIFIED"`. Cada reporte lleva tres secciones obligatorias y un
snapshot HTML cacheado de la pagina de licencia en el origen
(`snapshots/<id>.html`). El parser en `internal/license/parser.go`
lee las secciones linea por linea y rechaza el reporte si falta
cualquier campo obligatorio o esta mal formado.

Para los detalles del parser y del formato del reporte ver
`verification/README.md` y `openspec/specs/catalog-seed/spec.md`.

## Estado del seed

Las cuatro pistas del seed actual cargan `license_status: "VERIFIED"`
y un checksum real (no el SHA-256 del input vacio de 64 ceros que
el slice #1 usaba como placeholder detectable). Las fuentes se
verificacion se citan en cada `verification/<id>.md` y los snapshots
HTML cacheados viven en `verification/snapshots/`.

Reemplazar cualquier pista es un PR separado: actualizar
`manifest.json`, agregar o reemplazar el subdirectorio `<id>/`,
firmar un nuevo `verification/<id>.md` y refrescar el
`snapshots/<id>.html`. El pipeline de release (PR-E) detecta el
nuevo SHA de `manifest.json` automaticamente cuando se publica un
tag `v*`.
