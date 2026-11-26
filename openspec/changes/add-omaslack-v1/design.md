# Design — add-omaslack-v1

## Context

- El objetivo es Slack con UI (shell Quattro de Omarchy) y con TUI, ambas con funciones completas y estado compartido.
- Restricciones:
  - Los plugins QML corren sin sandbox dentro de `omarchy-shell`.
  - La API oficial castiga a las apps que no son del Marketplace (`conversations.history` y `conversations.replies` limitados a 1 llamada por minuto), con la excepción de las apps internas.
  - El protocolo de la app de escritorio no es oficial.
- Partes interesadas: usuarios de Omarchy con uno o varios workspaces y admins de workspace (si se usa el proveedor `internal-app`).

## Goals / Non-Goals

**Goals**
- Un solo motor (daemon) y dos clientes con paridad de funciones de mensajería.
- Tiempo real en los dos proveedores.
- Que la ventana, la barra y la TUI coincidan siempre en estado de lectura, borradores y no leídos.
- Seguir el tema de Omarchy en las dos caras, con recarga en caliente.
- Consumo menor a 100 MB de RSS para el daemon con dos workspaces activos. Es un objetivo, no una garantía.

**Non-Goals (v1)**
- Huddles y llamadas, Workflow Builder, Slack Connect (como gestión), administración de apps y de emoji personalizados, y Canvas con edición.
- Windows y macOS. Es un producto para Omarchy (Arch + Hyprland).
- Sandbox del plugin. Depende de Omarchy.

## Architecture

```
                 ┌──────────── omaslack daemon (systemd --user) ────────────┐
Slack  ◄──────►  │ Provider: desktop-session │ internal-app                 │
                 │   └─ Capabilities{realtime, search, files, presence,…}  │
                 │ Sync engine → Store (SQLite: WAL)                        │
                 │ Notifier · Presence/DND · RateBudget · Keyring           │
                 │ RPC: JSON-RPC 2.0, NDJSON, $XDG_RUNTIME_DIR/omaslack.sock│
                 └───────────────▲──────────────────────────▲───────────────┘
                                 │                          │
             omaslack tui (bubbletea v2)        plugin QML (omarchy-shell)
             omaslack ctl (scripts/hooks)       Service.qml ─ único cliente RPC
                                                ├ BarWidget.qml (badges)
                                                ├ Panel.qml (vista rápida)
                                                └ Window.qml (FloatingWindow)
```

### Binario único

`omaslack` tiene los subcomandos `daemon`, `tui`, `ctl <método> [json]`, `auth add|list|remove` y `doctor`. Un solo artefacto simplifica el AUR y garantiza que cliente y servidor hablen la misma versión del protocolo.

### Interfaz Provider

```go
type Provider interface {
    Capabilities() Capabilities          // qué soporta este proveedor
    Connect(ctx) (<-chan Event, error)   // tiempo real, normalizado al dominio
    History(ctx, conv, cursor) (Page, error)
    Replies(ctx, conv, threadTS, cursor) (Page, error)
    Post / Update / Delete / React / Upload / MarkRead / Search / SetPresence / SetDND ...
}
```

- `desktop-session` usa el token xoxc y la cookie xoxd, y abre el WebSocket del cliente web. Se porta de slk: `internal/slackdesktop`, `internal/slack/connection.go` y `events.go`.
- `internal-app` usa el user token xoxp sobre la Web API (slack-go) y recibe eventos por Socket Mode con un app token xapp. Lo que el proveedor no soporte se declara en `Capabilities`, y los clientes ocultan o deshabilitan esa acción.

### Store

SQLite en `$XDG_DATA_HOME/omaslack/<workspace>.db`, modo WAL. Tablas principales:

| Tabla | Clave | Notas |
|---|---|---|
| `workspaces` | team_id | nombre, url, provider, estado de la conexión |
| `users` | (team_id, user_id) | perfil, avatar_path, presencia |
| `conversations` | (team_id, conv_id) | tipo, nombre, sección, mute, `last_read`, `unread_count`, `mention_count` |
| `messages` | (team_id, conv_id, ts) | `thread_ts`, autor, texto mrkdwn, `blocks_json`, `edited_ts`, `deleted` |
| `reactions` | (team_id, conv_id, ts, emoji, user) | |
| `files` | (team_id, file_id) | metadatos y `local_path` en la cache |
| `drafts` | (team_id, conv_id, thread_ts) | **compartido entre la TUI y la UI** |
| `kv` | key | cursores de sync y frecencia de emoji |

Slack sigue siendo la fuente de verdad remota. El Store es una cache que se puede reconstruir.

### Protocolo local (daemon-api)

- Transporte: unix socket `$XDG_RUNTIME_DIR/omaslack.sock` con permisos 0600, framing NDJSON (un objeto JSON por línea) y JSON-RPC 2.0.
- Handshake: `hello{client, protocol: 1}` devuelve `{protocol, daemonVersion, workspaces[], capabilities}`. Si la versión mayor no coincide, se responde con error `-32001 incompatible_protocol`.
- Métodos (v1):
  - `workspaces.list`.
  - `conversations.list{team, filter}`, `conversations.open{team, conv}`, `conversations.markRead{team, conv, ts}`.
  - `messages.history{team, conv, before?, limit}`, `messages.replies{…, threadTS}`, `messages.send{team, conv, threadTS?, text, files?, broadcast?}`, `messages.edit` y `messages.delete`.
  - `reactions.add` y `reactions.remove`.
  - `files.upload{team, conv, paths[], caption}` y `files.fetch{team, fileId}` (devuelve `local_path`).
  - `drafts.get` y `drafts.set`.
  - `search.messages{team, query}`, `users.search{team, q}`.
  - `presence.set{team, auto|away}`, `dnd.snooze{team, minutes}`, `dnd.end`.
  - `unread.summary{}`: el agregado que usa la barra.
  - `ui.focus{team, conv, ts?}`: pide a un cliente que abra una conversación, para deep links y para el clic en una notificación.
- Eventos: `events.subscribe{topics[]}` convierte la conexión en un stream de notificaciones JSON-RPC. Tópicos:
  - `message.created|updated|deleted`, `reaction.changed`, `conversation.updated`.
  - `read.changed`, `draft.changed`, `typing`, `presence.changed`.
  - `connection.state`, `unread.summary`, `ui.focus`.
- Coalescencia: `unread.summary` sale como mucho una vez cada 250 ms, para no inundar la barra.

### Clientes

- **TUI**: es un cliente RPC más. Puede arrancar el daemon si no está corriendo (`systemctl --user start omaslack` o, si falla, un proceso hijo). Hay paridad de teclas con slk donde tenga sentido, para que la curva de aprendizaje sea nula para sus usuarios.
- **Plugin QML**:
  - `Service.qml` (`kinds: service`) mantiene **una sola** conexión con el socket, mediante `Quickshell.Io` `Socket`. Si el framing de líneas resulta poco confiable, se usa un `Process` con `omaslack ctl --stream`, que hace de puente por stdout y stdin.
  - Expone modelos (`ListModel`) a `BarWidget`, `Panel` y `Window`.
  - Con esto se evita el bug SLACK-1 de `janrenz.omarchy.slack` (GAPS.md), donde cada superficie tenía su propio Service y el estado de lectura quedaba desfasado.

### Theming

- **UI**: sólo usa `Style` (colores y fuentes del shell), así que cambia junto con el tema de Omarchy sin hacer nada.
- **TUI**:
  - Se instala la plantilla `~/.config/omarchy/themed/omaslack.toml.tpl`, con variables `{{ background_strip }}`, `{{ foreground_strip }}`, `{{ blue_strip }}`, etc.
  - Omarchy la renderiza en `~/.local/state/omarchy/current/theme/omaslack.toml`.
  - El hook `~/.config/omarchy/hooks/theme-set.d/omaslack` manda `SIGUSR1` a `omaslack tui`, igual que hace slk en `themereload_unix.go`.
  - Si no existe la plantilla, la TUI lee `colors.toml` del tema activo.

### Seguridad

- Los tokens se guardan sólo en el Secret Service, con el atributo `service=omaslack`. Si no hay Secret Service, van a `$XDG_CONFIG_HOME/omaslack/credentials` con permisos 0600 y se muestra un aviso visible.
- Ningún método RPC devuelve tokens. `doctor` los muestra enmascarados.
- El socket es 0600 en `$XDG_RUNTIME_DIR`, así que sólo el usuario dueño puede usarlo. Además se verifica `SO_PEERCRED`.
- Los archivos descargados van a `$XDG_CACHE_HOME/omaslack/files`, con nombres saneados.
- Los links salen siempre por `xdg-open`, y nunca se ejecuta contenido de los mensajes.

## Decisions

| Decisión | Elegida | Alternativas descartadas y por qué |
|---|---|---|
| Base de código | Desde cero, portando piezas MIT de slk | **Fork de slk**: son unas 107k líneas acopladas a bubbletea (los eventos son `tea.Msg`), separarlas es casi reescribirlas y además divergiría de upstream. **slk sin tocar + companion**: la UI no sería un cliente completo y dependería del esquema interno de la cache de slk. |
| Conexión | Las dos, a elección del usuario | Sólo la oficial: inviable sin app interna (1 llamada por minuto). Sólo la de escritorio: riesgo de términos de uso sin alternativa. |
| Lenguaje del daemon | Go | Python (como janrenz y petrzpav): más lento para el WebSocket, el render y SQLite, y no permitiría portar código de slk. C++/Qt: compartiría toolkit con la UI, pero no sirve para la TUI. |
| IPC | JSON-RPC 2.0 + NDJSON sobre unix socket | D-Bus: Quickshell no tiene un cliente D-Bus genérico cómodo en QML, y es más pesado. HTTP local: superficie de ataque innecesaria. |
| Ventana completa | `FloatingWindow` de Quickshell dentro del plugin | App Qt aparte, al estilo de la skill omarchy-app: más trabajo, y el requisito es que sea un plugin. Queda como posible evolución. |
| Specs | OpenSpec | SpecKit: su constitución y su flujo por feature encajan peor con capacidades que evolucionan por deltas. |

## Risks / Trade-offs

| Riesgo | Prob. | Impacto | Mitigación |
|---|---|---|---|
| Slack cambia el protocolo del cliente web | Media | Alto | `Provider` aislado y tests con eventos grabados. `internal-app` funciona como plan B y la migración es un comando (`auth add` + `auth remove`). |
| Términos de uso o políticas corporativas | Media | Alto | `desktop-session` sólo con aceptación explícita y un aviso en `doctor`. Se documenta la ruta de app interna para el admin. |
| Rate limits de la Web API | Alta en apps no internas | Medio | `RateBudget` por método y tier, prioridades (lo que pide el usuario va antes que el backfill) y respeto a `Retry-After`. |
| Cambios en la API de plugins de Omarchy (`schemaVersion` 1) | Media | Medio | Mantener chica la superficie QML. La lógica vive en el daemon. CI con `omarchy plugin validate` y `qmllint` contra el shell actual. |
| Rendimiento de listas largas en QML | Media | Medio | `ListView` virtualizado, páginas de 50 mensajes y render de mrkdwn a texto enriquecido hecho en el daemon. |
| Imágenes en la TUI | Alta (Alacritty no soporta gráficos) | Bajo | Protocolo kitty o sixel según el terminal, half-block como fallback (como slk), y opción para abrir la imagen en `imv`. |
| Plugin sin sandbox | Baja | Alto | El QML no maneja tokens ni descarga contenido de Slack: sólo pide `local_path` al daemon. |

## Migration Plan

No aplica, porque es un proyecto nuevo. Para quien venga de slk: `omaslack auth add --from-slk` puede importar los workspaces que ya tenga configurados en slk, leyendo `~/.config/slk/`. Es opcional y queda para después del MVP.

## Open Questions

1. ~~¿Quickshell `Socket` entrega lecturas por línea de forma confiable para un stream largo?~~ **Resuelta (2026-11-26):** sí. `Socket` + `SplitParser` pasó 100.000 eventos y 16 MB sin pérdidas, con una pausa máxima de 33 ms en el event loop. No hace falta el puente `ctl --stream`. Los reintentos de conexión necesitan backoff para no llenar el log. Detalle en `docs/spikes/m4.1-quickshell-socket.md`.
2. ¿El servicio de notificaciones del shell (`omarchy.notifications`) permite acciones con callback? Si no, se usa `notify-send --action` y el clic llega con `ui.focus`.
3. ¿El repo del plugin va separado o como `manifestPath` dentro del monorepo? Depende de lo que acepte el marketplace para fuentes con binario externo (`installation.mode: manual`).
