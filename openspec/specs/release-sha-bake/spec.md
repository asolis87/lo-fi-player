# Spec release-sha-bake

## Proposito

Define el contrato para inyectar el SHA de commit real de
`catalog/v1/manifest.json` en la variable `FirstRunCommitSHA` al
momento del release, y para verificar contra ese SHA el binario
publicado. La inyeccion es automatica via un GitHub Action que corre
en tags `v*` y produce un binario de release cuyo SHA horneado
coincide con el commit del manifest. La verificacion es automatica
via el gate `verify-release-binary` (slice #2 / PR-E) y via
`scripts/verify-release.sh` en CI.

## Requisitos

### Requisito: Inyeccion de SHA en tags de release

Un GitHub Action `release.yml` DEBE correr en cada push de tag `v*`.
La accion DEBE leer el SHA del commit que toco por ultima vez a
`catalog/v1/manifest.json` en HEAD, pasarlo a `go build` via
`-ldflags "-X github.com/asolis87/lo-fi-player/internal/catalog.FirstRunCommitSHA=<sha>"`,
y producir un binario de release cuyo `FirstRunCommitSHA` runtime
coincida con el commit del manifest.

#### Escenario: un tag produce un binario correctamente horneado

- DADO un tag `v0.2.0` pusheado al branch por defecto
- Y el `catalog/v1/manifest.json` actual esta en el commit
  `abc123…` (40 chars hex)
- CUANDO corre la accion `release.yml`
- ENTONCES se construye un binario con `FirstRunCommitSHA ==
  "abc123…"` (verificable mediante `lofi verify-release-binary`)
- Y el binario se adjunta al GitHub Release para `v0.2.0`

#### Escenario: override manual documentado

- DADO que un maintainer necesita publicar un binario sin tag
- CUANDO consulta `CONTRIBUTING.md`
- ENTONCES encuentra documentado el comando `go build -ldflags …`
- Y el ejemplo usa un SHA real de 40 chars hex, no un placeholder

### Requisito: Extraccion del simbolo runtime para verificar el SHA

El gate de release NO DEBE depender de un escaneo de bytes
(`strings <bin> | grep`, `grep -a`, ni busquedas de substring sobre
el archivo binario). Esa estrategia esta estructuralmente rota: el
linker de Go conserva el literal `<PLACEHOLDER_SHA>` del codigo
fuente en rodata adyacente al valor runtime reescrito de la
variable, asi que toda busqueda por substring marca como fallido a
un binario correctamente horneado.

El gate DEBE extraer el valor runtime del simbolo
`FirstRunCommitSHA` desde la tabla de simbolos del binario, seguir
el puntero de datos del struct `{Data uintptr; Len int}` hasta
rodata, y comparar los bytes resultantes contra el SHA esperado.
La implementacion vive en `internal/catalog.VerifyBakedSHA` y
soporta los tres formatos:

- Mach-O: macOS, `GOOS=darwin`.
- ELF: Linux, `GOOS=linux`.
- PE: Windows, `GOOS=windows` (paths terminados en `.exe`).

#### Escenario: binario correctamente horneado pasa el gate

- DADO un binario construido con
  `-ldflags "-X ...FirstRunCommitSHA=$MANIFEST_SHA"`
- Y `$MANIFEST_SHA` es un SHA de 40 chars hex
- CUANDO corre `lofi verify-release-binary <bin> $MANIFEST_SHA`
- ENTONCES la extraccion del simbolo runtime devuelve `$MANIFEST_SHA`
- Y el comparador devuelve nil
- Y el subcomando imprime `RELEASE OK: <sha> baked in <bin>` y sale 0

#### Escenario: SHA runtime placeholder bloquea el release

- DADO un binario construido SIN `-ldflags`
- Y el valor runtime de `FirstRunCommitSHA` sigue siendo
  `<PLACEHOLDER_SHA>` (literal del codigo fuente)
- CUANDO corre `lofi verify-release-binary <bin> <cualquier-sha>`
- ENTONCES la extraccion del simbolo runtime devuelve
  `<PLACEHOLDER_SHA>`
- Y el comparador envuelve `ErrPlaceholderFound`
- Y el subcomando sale 1
- Y el mensaje de error nombra el path del binario y el valor
  runtime observado

#### Escenario: SHA runtime distinto del esperado bloquea el release

- DADO un binario construido con
  `-ldflags "-X ...FirstRunCommitSHA=<sha-horneado>"`
- Y el operador corre el gate contra un SHA esperado distinto
  `<sha-esperado>`
- CUANDO corre `lofi verify-release-binary <bin> <sha-esperado>`
- ENTONCES la extraccion del simbolo runtime devuelve `<sha-horneado>`
- Y el comparador envuelve `ErrSHAMismatch`
- Y el subcomando sale 1
- Y el mensaje de error nombra el path del binario, el valor
  runtime observado y el valor esperado

#### Escenario: binario ilegible bloquea el release

- DADO un path que no existe, no es un archivo regular, o no
  puede parsearse como Mach-O / ELF / PE
- CUANDO corre `lofi verify-release-binary <path> <sha>`
- ENTONCES la lectura falla
- Y el gate envuelve `ErrBinaryUnreadable`
- Y el subcomando sale 1
- Y el mensaje de error nombra el path ofensor

### Requisito: Equivalencia entre el gate bash y el subcomando Go

`scripts/verify-release.sh` DEBE ser un wrapper trivial sobre el
subcomando `lofi verify-release-binary`. El script bash NO DEBE
implementar logica de verificacion propia; debe forwardear el exit
code del subcomando y dejar que la salida humana la genere el
subcomando Go. Asi las pruebas unitarias Go y el pipeline CI
ejercitan la misma ruta de codigo.

#### Escenario: el script bash delega en el subcomando Go

- DADO un binario en `<bin>` y un SHA esperado `<sha>`
- CUANDO corre `./scripts/verify-release.sh <bin> <sha>`
- ENTONCES el script invoca
  `go run ./cmd/lofi verify-release-binary <bin> <sha>`
- Y propaga el exit code del subcomando (0 OK, 1 falla, 2
  invocacion invalida)
- Y el mensaje `RELEASE OK` o `RELEASE BLOCKED` que imprime el
  script viene del subcomando, no de logica bash

### Requisito: `FirstRunCommitSHA` es `var`, no `const`

`FirstRunCommitSHA` DEBE seguir siendo una `var` en
`internal/catalog/pinning.go` para permitir que `-ldflags -X` la
sobreescriba al momento de build. El codigo de produccion NO DEBE
reasignarla; el pipeline de release es el unico escritor sancionado.
La razon es mecanica: `const` no es overridable por `-ldflags -X` en
el linker de Go.

#### Escenario: override en test es permitido

- DADO un test que pone
  `catalog.FirstRunCommitSHA = "<valid 40-hex SHA>"`
- CUANDO `ValidateURL` corre contra una URL construida con ese SHA
- ENTONCES la validacion pasa
- Y el test no modifica ningun otro estado global
