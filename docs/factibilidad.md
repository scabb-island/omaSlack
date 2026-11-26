# Factibilidad — omaslack: Slack "Omarchy Way" con UI y TUI

Fecha: 2026-11-26 · Estado: propuesta · Specs: [`openspec/changes/add-omaslack-v1`](../openspec/changes/add-omaslack-v1/)

## 1. Veredicto

**Es factible**, con riesgos conocidos y acotados.

La condición de que funcione por UI y por TUI se cumple con una arquitectura de **daemon local + dos clientes finos**:

- un **plugin QML** para el shell Quattro de Omarchy, con barra, panel y ventana;
- una **TUI en Go** con bubbletea.

Los dos comparten estado (no leídos, borradores, lecturas) a través de un unix socket.

Ninguna de las piezas es experimental:

- slk (MIT) demuestra que Go + bubbletea + el protocolo de la app de escritorio dan un cliente de Slack completo.
- Los cinco plugins de Slack del marketplace demuestran que el shell aguanta una ventana de chat en QML, con un ícono de barra, un Service y procesos auxiliares.

El riesgo real no es técnico sino **de plataforma**:

- el protocolo no oficial de Slack y sus términos de uso;
- los límites de la API oficial;
- una API de plugins de Omarchy que todavía es joven.

Los tres se mitigan con el diseño (sección 6).

## 2. Análisis de slk (gammons/slk)

Fuente: `github.com/gammons/slk` (clonado el 2026-11-26).

| Aspecto | Hallazgo | Fuente |
|---|---|---|
| Stack | Go 1.26, bubbletea/lipgloss/bubbles v2, slack-go 0.23, modernc sqlite, gorilla/websocket, godbus | `go.mod` |
| Tamaño | ~107k líneas de Go sin contar tests (el wiki dice ~9.3k, está desactualizado) | `wc -l` sobre `*.go` |
| Capas | UI (bubbletea) → Service → Client (Web API + WebSocket del navegador) → Data (SQLite, TOML, tokens) | `wiki/Architecture.md` |
| Puertos | Las interfaces que consume la TUI están en un solo archivo (512 líneas) | `internal/core/ports.go` |
| Auth | Lee la sesión de la app de escritorio (cookies, leveldb, Secret Service y KWallet) o un cURL del navegador; tokens xoxc/xoxd | `internal/slackdesktop/`, `README.md` |
| Tiempo real | WebSocket del protocolo del navegador (no RTM ni Socket Mode), reconexión con backoff de 1 a 30 s | `docs/STATUS.md` |
| IPC | Socket de control sólo para abrir permalinks | `cmd/slk/control_socket.go` |
| Temas | 59 temas, TOML en `~/.config/slk/themes/`, recarga con `SIGUSR1` | `wiki/Configuration.md`, `cmd/slk/themereload_unix.go` |
| Non-goals | Huddles, Slack Connect, Workflow Builder, gestión de apps y emoji | `wiki/Tradeoffs-and-Non-Goals.md` |
| Licencia | MIT | `LICENSE` |

**Qué tomamos (con atribución MIT):**
- la lectura de la sesión de escritorio;
- el cliente WebSocket y el manejo de eventos;
- el render de mrkdwn;
- el pipeline de imágenes (kitty, sixel, half-block);
- el diseño de teclas.

**Por qué no hacemos fork** (decisión del usuario, que coincide con el análisis): la lógica de slk está acoplada a bubbletea, porque los eventos del WebSocket viajan como `tea.Msg` dentro de `cmd/slk`. Convertirla en daemon equivale a reescribir la capa de servicios, y el fork quedaría divergiendo de upstream para siempre.

## 3. Lo que ya existe en el marketplace de Omarchy

Fuente: `omacom/omarchy-plugin-marketplace/registry.json` (7091 fuentes). Se clonaron y leyeron los cinco plugins de Slack:

| Plugin | Qué es | Conexión | Tiempo real | UI | TUI |
|---|---|---|---|---|---|
| `janrenz.omarchy.slack` | Barra, desplegable y ventana `FloatingWindow` en QML, con un helper Python (~14k líneas QML/Py) | API oficial (OAuth con la app que trae el plugin o una propia) | No: polling cada 120 s, diseñado alrededor de 1 llamada por minuto a `conversations.history` | Sí | No |
| `bottelet.slack` | Barra y panel con ventana | API oficial (token propio u OAuth vía worker) | No: polling | Sí | No |
| `petrzpav.slack` | TUI en Python + `slack-sync.service` + widget de barra | App interna con Socket Mode | Sí | Sólo barra | Sí |
| `leonavas.slack` | Widget de barra que maneja la TUI "slick" (Rust) en el scratchpad | Delegada a slick | Depende de slick | Sólo barra | Externa |
| `io.github.thisisgm.slack` | Selector de disponibilidad (Available, Focus, Away) | App propia, 4 scopes | No | Panel mínimo | No |

**Hueco que cubre omaslack:** ningún plugin ofrece **un mismo motor** que alimente una UI nativa completa **y** una TUI con estado sincronizado y en tiempo real. Además, en `janrenz.omarchy.slack/GAPS.md` (SLACK-1) quedó documentado lo que pasa sin un store central: la barra sigue mostrando un no leído hasta 120 s después de que la ventana lo marcó como leído. omaslack lo evita por diseño, porque el daemon es la única fuente de verdad.

**Patrones que conviene reutilizar:**
- Un Service QML único que lanza un helper (`janrenz`, `gardnmi/omarchy-irc`).
- `FloatingWindow` como ventana real de Hyprland (`janrenz`).
- La configuración en `barWidget.schema[]`.
- Los tokens en `secret-tool` (`thisisgm`, `bottelet`).
- El estacionamiento en el scratchpad (`leonavas`), que sirve como idea para la TUI.

## 4. Contrato de plugin de Omarchy (resumen)

Fuentes: `plugins.omarchy.org/develop.html`, `publish.html`, `/usr/share/omarchy/shell/` y `omarchy plugin list --json` en esta máquina.

- `manifest.json`: `schemaVersion`, `id` (con namespace, nunca `omarchy.*`), `name`, `version`, `author`, `description`, `kinds` (`bar-widget`, `panel`, `overlay`, `menu`, `service`, `bar`), `entryPoints` y `barWidget{displayName, category, defaults, schema[]}`.
- QML sobre Quickshell (`Quickshell`, `Quickshell.Io`, `qs.Commons`, `qs.Ui`), con colores del singleton `Style`.
- El IPC es `omarchy-shell shell toggle|summon|hide <id>`. Para el ciclo de vida: `omarchy plugin add|remove|validate|list`.
- **Corre sin sandbox** dentro del proceso del shell. No puede arrancar otro Quickshell.
- Theming de terceros: plantillas `~/.config/omarchy/themed/*.tpl` (variables `{{ foreground_strip }}`, `{{ red_strip }}`, …), renderizadas en `~/.local/state/omarchy/current/theme/`, más hooks `~/.config/omarchy/hooks/theme-set.d/`.
- Apps de terminal: `omarchy-launch-tui` y `omarchy-launch-or-focus-tui`.

## 5. Conexión a Slack: los dos proveedores

| | `desktop-session` | `internal-app` |
|---|---|---|
| Credencial | xoxc + cookie xoxd (app de escritorio o cURL del navegador) | user token xoxp + app token xapp |
| Setup | Ninguno si la app de escritorio tiene la sesión iniciada | Crear una app desde el manifest e instalarla en el workspace (puede requerir un admin) |
| Tiempo real | WebSocket del cliente web | Socket Mode |
| Rate limits | Los del cliente web | Completos (las apps internas no tienen el límite de 1 llamada por minuto) |
| Funciones | Todas las de slk (secciones, actividad, huddles visibles, etc.) | Las de la Web API pública (sin secciones nativas, sin parte de la info interna) |
| Términos de uso | **No oficial**: puede violar los términos de uso de Slack. Se pide consentimiento explícito | Oficial |
| Riesgo de rotura | Medio: Slack puede cambiar el protocolo | Bajo |

Lo que un proveedor no soporte lo declara en `Capabilities`, y los clientes lo ocultan (spec `slack-connection`).

## 6. Riesgos y mitigaciones

| Riesgo | Mitigación |
|---|---|
| Slack cambia o bloquea el protocolo del cliente web | `Provider` aislado, tests con eventos grabados y migración en un comando a `internal-app` |
| Términos de uso o política corporativa (relevante si se usa en un workspace de trabajo) | Consentimiento explícito; documentar la ruta de app interna para que la apruebe un admin |
| Rate limits de la API oficial | `RateBudget` con prioridades y `Retry-After`; recomendar app interna |
| API de plugins joven (`schemaVersion` 1) | Superficie QML mínima, lógica en el daemon y CI con `omarchy plugin validate` + `qmllint` |
| Rendimiento de QML con historial largo | `ListView` virtualizado, paginado y mrkdwn renderizado en el daemon |
| Imágenes en la TUI (Alacritty no tiene gráficos) | Kitty o sixel si la terminal los soporta, half-block como fallback y visor externo |
| Plugins sin sandbox | El QML nunca toca tokens ni la red de Slack |
| Marca "Slack" | Nombre propio (omaslack); "Slack" sólo para describir la interoperabilidad, como hace slk |
| Competencia en el marketplace (5 plugins) | Diferenciarse por UI + TUI, tiempo real y estado único |

## 7. Esfuerzo estimado

**Es una estimación gruesa**, para una persona con asistencia de IA y portando piezas de slk. No sale de mediciones.

| Hito | Contenido | Semanas |
|---|---|---|
| M0 | Esqueleto, CI, manifest mínimo | 0,5 |
| M1 | Proveedores, keyring, Store, sync, RateBudget | 3–4 |
| M2 | API del daemon, systemd, `ctl` | 1 |
| M3 | TUI MVP | 3 |
| M4 | UI QML MVP (spike de socket, Service, barra, panel, ventana) | 3–4 |
| M5 | Notificaciones, presencia y DND, tema y hook, `doctor` | 1,5 |
| M6 | AUR, README, marketplace | 1 |
| **Total MVP** | | **~13–15 semanas** |

El camino crítico es M1 → M2, del que dependen los dos clientes. Una vez estable M2, M3 y M4 pueden avanzar en paralelo.

## 8. Fuera de alcance (v1)

Huddles y llamadas, Workflow Builder, Slack Connect como gestión, administración de apps y de emoji personalizados, edición de Canvas, y otras plataformas fuera de Omarchy.

## 9. Por qué OpenSpec y no SpecKit

El proyecto se divide en **capacidades independientes** (conexión, sync, API, mensajería, notificaciones, TUI, UI, integración) que van a evolucionar con cambios incrementales: un proveedor nuevo, cambios en la API de plugins. El modelo de OpenSpec encaja directamente:

- `specs/` como verdad vigente;
- `changes/` con deltas ADDED, MODIFIED y REMOVED;
- `archive` al mergear.

SpecKit pone el foco en la constitución y en un flujo lineal por feature, y es más pesado para mantener un sistema de varias piezas a lo largo del tiempo.

Estado: `openspec validate add-omaslack-v1 --strict` pasa con **59 requirements en 8 capacidades** (OpenSpec 2.2.0).

## 10. Próximos pasos

1. ~~Definir el namespace del plugin.~~ Resuelto: `ar.scabb.omaslack` (DNS invertido de `scabb.ar`; libre en el registry al 2026-11-26).
2. ~~Confirmar con el admin del workspace qué proveedor está permitido.~~ Resuelto (2026-11-26): el admin no tiene objeciones.
3. Hacer el spike de M4.1 (Quickshell `Socket` frente al puente `ctl --stream`) en paralelo con M1, porque es la incógnita técnica más grande de la UI.
4. Arrancar M0.
