# Change: add-omaslack-v1 — cliente de Slack "Omarchy Way" con UI y TUI sobre un daemon común

## Why

El cliente oficial de Slack es una app Electron que ocupa entre 500 MB y 1,5 GB de RAM, no sigue el tema del sistema y no está pensada para el teclado, lo opuesto a lo que propone Omarchy. Hay alternativas, pero cada una cubre sólo una parte:

- **slk** (gammons/slk) es una TUI excelente, pero no tiene ninguna integración con el shell de Omarchy.
- Los cinco plugins de Slack del marketplace de Omarchy (`janrenz.omarchy.slack`, `bottelet.slack`, `petrzpav.slack`, `leonavas.slack`, `io.github.thisisgm.slack`) se quedan en una de estas situaciones:
  - Son sólo UI y están limitados por la API oficial: 1 consulta de historial por minuto y sin tiempo real.
  - Son sólo un ícono de barra que maneja una TUI externa.
  - Cubren una sola función, como presencia y DND.

Ninguno ofrece un **motor compartido** que alimente a la vez una UI nativa completa y una TUI con estado sincronizado. Esta propuesta cubre ese hueco.

## What Changes

- **ADDED** `slack-connection`: interfaz `Provider` con dos implementaciones a elección del usuario. Una es `desktop-session` (sesión de la app de escritorio o del navegador: tiempo real completo y cero setup, pero no oficial). La otra es `internal-app` (user token de una app interna con Socket Mode: oficial, sin el límite de 1 llamada por minuto, pero requiere aprobación de un admin). Incluye multi-workspace y credenciales en el keyring.
- **ADDED** `sync-engine`: tiempo real, reconexión con backoff, backfill, cache SQLite y presupuesto de rate limit.
- **ADDED** `daemon-api`: JSON-RPC 2.0 sobre un unix socket privado, stream de eventos, versión de protocolo y unit de systemd de usuario.
- **ADDED** `messaging`: leer, enviar, editar, borrar, hilos, reacciones, archivos, menciones, búsqueda, borradores y estado de lectura únicos para ambos clientes.
- **ADDED** `notifications-presence`: notificaciones de escritorio, reglas, DND y presencia.
- **ADDED** `tui-client`: cliente de terminal con modos tipo vim, tema de Omarchy y recarga en caliente.
- **ADDED** `gui-client`: plugin QML con Service, BarWidget, Panel de vista rápida y ventana completa.
- **ADDED** `omarchy-integration`: manifest, plantilla de tema con hook, atajos de teclado, entrada en el launcher, AUR y publicación en el marketplace.

## Impact

- **Specs afectadas:** las 8 capacidades anteriores. Todas son nuevas.
- **Código:** proyecto nuevo. Repo `omaslack` (binario Go) y directorio `plugin/` (plugin de Omarchy), que se publica como repo separado o en un subdirectorio con `manifestPath`.
- **Usuarios:** tienen que elegir proveedor al hacer `omaslack auth add`. El proveedor `desktop-session` muestra un aviso sobre los términos de uso que hay que aceptar.
- **Riesgos principales:** que Slack cambie el protocolo no oficial, cambios en la API de plugins de Omarchy y la falta de sandbox de los plugins. El detalle está en `docs/factibilidad.md` y `design.md`.
- **Atribución:** se porta código MIT de slk (auth de escritorio, cliente WebSocket, render de mrkdwn) con su aviso de copyright.
