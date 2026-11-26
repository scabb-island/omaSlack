# Project Context — omaslack

## Purpose

omaslack es un cliente de Slack "Omarchy Way": liviano, manejable con el teclado y con el tema del sistema. Ofrece dos caras sobre el mismo motor:

- **UI**: un plugin del shell Quattro de Omarchy (Quickshell/QML) que aporta ícono con badges en la barra, panel de vista rápida y ventana completa.
- **TUI**: un cliente de terminal (bubbletea) con la misma funcionalidad de mensajería, lanzado con `omarchy-launch-tui`.

Los dos clientes son vistas finas sobre un daemon local (`omaslack daemon`), que es la única pieza que habla con Slack, guarda credenciales y mantiene el estado.

Condición de diseño innegociable: **toda capacidad de mensajería tiene que poder usarse por UI y por TUI.**

**ID del plugin:** `ar.scabb.omaslack`, el DNS invertido de `scabb.ar`. Los IDs del marketplace son permanentes: no se cambia.

## Tech Stack

| Pieza | Tecnología |
|---|---|
| Daemon + CLI + TUI | Go 1.26, un solo binario `omaslack` con subcomandos `daemon`, `tui`, `ctl`, `auth` |
| TUI | `charm.land/bubbletea/v2`, `lipgloss/v2`, `bubbles/v2` |
| Slack | `github.com/slack-go/slack` (Web API, Socket Mode) + cliente WebSocket propio (`gorilla/websocket`) para el protocolo del navegador |
| Cache | SQLite vía `modernc.org/sqlite` (sin cgo) |
| Credenciales | Secret Service (`secret-tool`/D-Bus, `godbus/dbus/v5`), con fallback a un archivo 0600 |
| UI | QML sobre Quickshell (shell Quattro de Omarchy): `Quickshell`, `Quickshell.Io`, `qs.Commons`, `qs.Ui` |
| Empaquetado | goreleaser + PKGBUILD (AUR) para el binario; repo git del plugin para `omarchy plugin add` |

## Project Conventions

### Code Style
- Go: `gofmt`, `go vet ./...` y `golangci-lint`. Paquetes bajo `internal/` organizados por capacidad: `provider/`, `sync/`, `store/`, `rpc/`, `notify/`, `tui/`.
- QML: validar con `qmllint -I "$OMARCHY_PATH/shell"`. Colores, fuentes y espaciados salen sólo del singleton `Style`, sin hex hardcodeados.
- Cualquier código portado de slk (MIT, © Grant Ammons) lleva un encabezado de atribución en el archivo, y el aviso se repite en `THIRD_PARTY_NOTICES.md`.

### Architecture Patterns
- **Daemon como fuente única de verdad.** Los clientes no guardan estado de Slack propio: ni estado de lectura, ni borradores, ni cache.
- **Puertos y adaptadores.** La conexión a Slack es la interfaz `Provider`, con una implementación por método de conexión, y cada una declara qué capacidades soporta.
- **Event sourcing liviano.** El daemon publica eventos de dominio (`message.created`, `conversation.read`, etc.) y los clientes se actualizan reaccionando a ellos.

### Testing Strategy
- Unit tests de Go por paquete. Para el sync se usa un `Provider` falso que reproduce eventos grabados.
- Tests de contrato del protocolo JSON-RPC: para cada método, un golden de request y response.
- Tests de QML con `qmltestrunner` sobre los modelos JS/QML.
- Tests golden de la TUI (snapshot de la vista renderizada).

### Git Workflow
- Trunk-based. Cada cambio de comportamiento entra con su cambio OpenSpec (`openspec/changes/<id>`) y se archiva al mergear.

## Domain Context
- **Workspace (team):** una organización de Slack. Un usuario puede tener varias activas.
- **Conversation:** un canal público o privado, un DM (`im`) o un DM grupal (`mpim`).
- **Thread:** respuestas a un mensaje raíz (`thread_ts`).
- **xoxc/xoxd:** token y cookie de la sesión de la app de escritorio o del navegador, que usa el proveedor `desktop-session`.
- **xoxp:** user token de una app de Slack, que usa el proveedor `internal-app`.
- **App interna:** una app creada e instalada sólo en el propio workspace. No le aplican los límites de uso de 2025 para apps fuera del Marketplace, que dejan `conversations.history` en 1 llamada por minuto.

## Important Constraints
- Los plugins de Omarchy corren **sin sandbox** dentro del proceso `omarchy-shell`. Por eso el QML nunca maneja tokens, y lo pesado (red, parseo) queda en el daemon.
- La API de plugins está en `schemaVersion: 1` y es joven, así que la UI tiene que aislar lo que dependa del shell.
- El proveedor `desktop-session` usa un protocolo no oficial que puede violar los términos de uso de Slack. Se ofrece como opción explícita con aviso al usuario y nunca se activa por defecto sin consentimiento.
- La marca "Slack" sólo se usa para describir interoperabilidad. No va en el nombre del producto ni en su logo.

## External Dependencies
- Slack Web API, Socket Mode y el WebSocket del cliente web (no oficial).
- Shell Quattro de Omarchy (`omarchy-shell`, `omarchy plugin`), plantillas `~/.config/omarchy/themed/*.tpl` y hooks `~/.config/omarchy/hooks/theme-set.d/`.
- Secret Service (gnome-keyring o KWallet).
