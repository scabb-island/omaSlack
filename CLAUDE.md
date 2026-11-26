# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Estado del proyecto

omaslack es un cliente de Slack "Omarchy Way" con UI (plugin QML del shell Quattro) y TUI (Go/bubbletea) sobre un daemon común. M0 (esqueleto y CI) está hecho y el spike M4.1 está resuelto. El próximo hito es M1 (proveedores y sync). Los subcomandos salvo `version` todavía devuelven "not implemented" (exit 3) e indican en qué hito llegan.

- Informe de factibilidad: `docs/factibilidad.md`. Resultados de spikes: `docs/spikes/`
- Contexto, stack y convenciones: `openspec/project.md`
- Cambio en curso: `openspec/changes/add-omaslack-v1/` (`tasks.md` marca el avance por hito)
- El ID del plugin en Omarchy es `ar.scabb.omaslack`. Es permanente en el marketplace, así que no se cambia.
- Repo: gitlab.com/scabb-island/omaslack (privado)

## Toolchain y comandos

Las herramientas están fijadas en `mise.toml`: Go 1.26, golangci-lint 2.14.0 y goreleaser 2.20.2. Dentro del directorio ya apuntan a esas versiones. Desde scripts, usá `mise exec -- <cmd>`.

```bash
make check            # vet + golangci-lint + test: exactamente lo que corre la CI
make local            # check + test -race + qmllint + omarchy plugin validate + OpenSpec
make build            # bin/omaslack con la versión inyectada por ldflags
make snapshot         # goreleaser snapshot (linux amd64/arm64) en dist/
go test ./internal/cli -run TestVersion   # un solo test
```

Para QML, `make qmllint` mapea `$OMARCHY_PATH/shell` como prefijo de import `qs` (así lo hace el shell) y apaga `missing-property`, porque el shell les pasa a los plugins un `bar` sin tipo estático.

## CI (GitLab): minutos contados

Los minutos de los runners compartidos son limitados. Mantené `.gitlab-ci.yml` así:

- Un solo job `check` con la imagen `golang:1.26-alpine`. golangci-lint se baja como binario con checksum fijo; no se compila.
- Corre sólo si cambió código Go o la CI. Los cambios de docs y specs no gastan minutos.
- No hay pipelines duplicados de rama y MR.
- La caché la escribe sólo `main`; los MRs sólo la leen.
- `release` corre sólo en tags `vX.Y.Z`.

Lo que necesita una máquina con Omarchy (qmllint contra el shell, `omarchy plugin validate`, OpenSpec) va en `make local`, nunca en la CI. Antes de cambiar la CI, simulá el job en local con `podman run ... golang:1.26-alpine` en vez de probar con pushes.

## Flujo con OpenSpec

Todo cambio de comportamiento entra como un cambio OpenSpec antes de tocar código. Al mergear se archiva.

```bash
npx -y @fission-ai/openspec@latest show add-omaslack-v1
npx -y @fission-ai/openspec@latest validate --all --strict      # también lo corre `make specs`
npx -y @fission-ai/openspec@latest archive add-omaslack-v1      # al completar tasks.md
```

Formato de las specs: la prosa va en español, pero los encabezados y las palabras normativas quedan en inglés para que pase el validador: `## ADDED Requirements`, `### Requirement:`, `#### Scenario:`, **SHALL/MUST** en el texto y bullets `- **WHEN**` / `- **THEN**`.

## Arquitectura (decisiones ya tomadas)

- **Un solo binario:** `cmd/omaslack` despacha subcomandos con `internal/cli`, cuyas dependencias se inyectan con `cli.Env` para testear sin tocar stdout. La versión de protocolo vive en `internal/version.Protocol`.
- **El daemon es la única fuente de verdad local.** Es lo único que habla con Slack y guarda credenciales. La TUI y el plugin QML son clientes finos sobre JSON-RPC 2.0 + NDJSON en `$XDG_RUNTIME_DIR/omaslack.sock`. No agregues estado de Slack en los clientes: así se evita el bug de "la barra no se entera de que la ventana marcó como leído" (SLACK-1 en el plugin janrenz).
- **Plugin (`plugin/`):** el shell monta `Service.qml` una sola vez y cada barra lo obtiene con `bar.shell.serviceFor("ar.scabb.omaslack")`. El Service se conecta directo al socket con `Quickshell.Io` `Socket` + `SplitParser`, validado en el spike M4.1. Los reintentos de conexión van con backoff, porque cada falla deja un WARN en el log del shell.
- **El QML corre sin sandbox** dentro de `omarchy-shell`: nada de tokens ni requests a Slack desde QML. Colores y fuentes salen sólo de `Style` y `Commons`.
- **Dos proveedores de conexión** detrás de `Provider`: `desktop-session` (xoxc/xoxd, no oficial, requiere consentimiento explícito) e `internal-app` (xoxp + Socket Mode). Lo que un proveedor no soporta se declara en `Capabilities`.
- **Hecho desde cero, no es un fork de slk.** Se porta código MIT de `gammons/slk` con encabezado de atribución en cada archivo y en `THIRD_PARTY_NOTICES.md`.
- **Paridad UI/TUI:** toda operación de mensajería tiene que estar en los dos clientes.
- **`spikes/`** tiene experimentos descartables que se corren a mano. golangci-lint les perdona gosec y noctx.
