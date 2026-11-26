# Tasks — add-omaslack-v1

## M0 — Esqueleto y CI
- [x] 0.1 `go mod init`, binario `omaslack` con subcomandos (`daemon`, `tui`, `ctl`, `auth`, `doctor`)
- [x] 0.2 golangci-lint, `go vet` y `go test` en CI (un solo job, sólo si cambia código Go). `qmllint`, `omarchy plugin validate` y OpenSpec en `make local`. goreleaser con snapshot local y release en tags
- [x] 0.3 `THIRD_PARTY_NOTICES.md` con la atribución MIT de slk
- [x] 0.4 Directorio `plugin/` con un `manifest.json` mínimo que pase `omarchy plugin validate`

## M1 — Conexión y sync
- [ ] 1.1 Interfaz `Provider` + `Capabilities` + eventos de dominio normalizados
- [ ] 1.2 `desktop-session`: portar la lectura de la sesión de escritorio (cookies, leveldb, Secret Service) y el login por cURL del navegador
- [ ] 1.3 `desktop-session`: WebSocket en tiempo real con reconexión y backoff (1–30 s)
- [ ] 1.4 `internal-app`: Web API con xoxp, Socket Mode con xapp y manifest de app de ejemplo
- [ ] 1.5 Keyring (Secret Service) con fallback 0600 y aviso
- [ ] 1.6 Store SQLite (WAL) con migraciones versionadas
- [ ] 1.7 Sync: arranque, backfill por conversación al abrirla y reconciliación al reconectar
- [ ] 1.8 `RateBudget` con prioridades y manejo de `Retry-After`
- [ ] 1.9 Tests con eventos grabados para los dos proveedores

## M2 — API del daemon
- [ ] 2.1 Socket 0600 + verificación `SO_PEERCRED` + JSON-RPC 2.0 sobre NDJSON
- [ ] 2.2 `hello` y negociación de versión de protocolo
- [ ] 2.3 Métodos v1 (ver design.md) con tests golden
- [ ] 2.4 `events.subscribe` con tópicos y coalescencia de `unread.summary`
- [ ] 2.5 Unit de systemd `omaslack.service` y `omaslack ctl` (incluido `--stream`)

## M3 — TUI MVP
- [ ] 3.1 Layout: rail de workspaces, sidebar, mensajes, hilo y compose
- [ ] 3.2 Modos NORMAL, INSERT y COMMAND, con teclas compatibles con slk
- [ ] 3.3 Buscador de conversaciones, búsqueda y selector de reacciones
- [ ] 3.4 Render de mrkdwn, menciones, emoji y separadores de día
- [ ] 3.5 Imágenes con kitty, sixel o half-block
- [ ] 3.6 Tema desde `omaslack.toml` de Omarchy + recarga con SIGUSR1
- [ ] 3.7 Tests golden de las vistas

## M4 — UI QML MVP
- [x] 4.1 Spike: Quickshell `Socket` frente al puente `ctl --stream`. Ganó `Socket` directo (ver `docs/spikes/m4.1-quickshell-socket.md`)
- [ ] 4.2 `Service.qml` como único cliente RPC y sus modelos
- [ ] 4.3 `BarWidget.qml`: ícono, badges de DM y de menciones, estado de conexión y tooltip
- [ ] 4.4 `Panel.qml`: lista de no leídos con respuesta rápida
- [ ] 4.5 `Window.qml` (`FloatingWindow`): sidebar, mensajes, hilos, compose, reacciones y adjuntos
- [ ] 4.6 Navegación completa por teclado y estilos sólo de `Style`
- [ ] 4.7 `schema[]` del manifest para los settings
- [ ] 4.8 Tests con `qmltestrunner` de los modelos

## M5 — Notificaciones, presencia e integración con Omarchy
- [ ] 5.1 Notifier: reglas (DM, mención, palabras clave, canales), DND y supresión si la conversación está a la vista
- [ ] 5.2 Clic en la notificación que lleva a `ui.focus` y abre el cliente activo (ventana o TUI)
- [ ] 5.3 Presencia y DND desde los dos clientes
- [ ] 5.4 Plantilla `omaslack.toml.tpl` + hook `theme-set.d/omaslack`
- [ ] 5.5 `.desktop` para el launcher (TUI) y atajos sugeridos en el README
- [ ] 5.6 `omaslack doctor`: proveedor, permisos, socket, tema y versión del shell

## M6 — Distribución
- [ ] 6.1 PKGBUILD (AUR `omaslack-bin`) + release con goreleaser
- [ ] 6.2 README del plugin con instrucciones de instalación y desinstalación, `LICENSE` y `preview.png`
- [ ] 6.3 Envío al marketplace (`SUBMISSION.md`) con `installation.mode: manual` si hace falta
- [ ] 6.4 `openspec archive add-omaslack-v1`
