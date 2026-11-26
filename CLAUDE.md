# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Estado del proyecto

omaslack es un cliente de Slack "Omarchy Way" con UI (plugin QML del shell Quattro) y TUI (Go/bubbletea) sobre un daemon común. **Todavía no hay código**: sólo están la propuesta y las specs.

- Informe de factibilidad: `docs/factibilidad.md`
- Contexto del proyecto, stack y convenciones: `openspec/project.md`
- Cambio inicial: `openspec/changes/add-omaslack-v1/` (`proposal.md`, `design.md`, `tasks.md` y `specs/<capacidad>/spec.md`)

El ID del plugin en Omarchy es `ar.scabb.omaslack`. Es permanente en el marketplace, así que no se cambia.

## Flujo con OpenSpec

Todo cambio de comportamiento entra como un cambio OpenSpec antes de tocar código. Al mergear se archiva.

```bash
npx -y @fission-ai/openspec@latest list                                   # cambios activos
npx -y @fission-ai/openspec@latest show add-omaslack-v1                   # ver un cambio
npx -y @fission-ai/openspec@latest validate add-omaslack-v1 --strict      # validar (tiene que pasar)
npx -y @fission-ai/openspec@latest archive add-omaslack-v1                # al completar tasks.md
```

Formato de las specs: la prosa va en español, pero los encabezados y las palabras normativas quedan en inglés para que pase el validador: `## ADDED Requirements`, `### Requirement:`, `#### Scenario:`, **SHALL/MUST** en el texto y bullets `- **WHEN**` / `- **THEN**`. Cada requirement necesita al menos un escenario.

## Arquitectura (decisiones ya tomadas)

- **Un daemon (`omaslack daemon`) es la única fuente de verdad local.** Es lo único que habla con Slack y guarda credenciales. La TUI y el plugin QML son clientes finos sobre JSON-RPC 2.0/NDJSON en `$XDG_RUNTIME_DIR/omaslack.sock`. No agregues estado de Slack en los clientes: así se evita el bug de "la barra no se entera de que la ventana marcó como leído" (SLACK-1 en el plugin janrenz).
- **Dos proveedores de conexión** detrás de la interfaz `Provider`: `desktop-session` (xoxc/xoxd, no oficial, requiere consentimiento explícito) e `internal-app` (xoxp + Socket Mode). Lo que un proveedor no soporta se declara en `Capabilities`, no se resuelve con condicionales en los clientes.
- **Hecho desde cero, no es un fork de slk.** Se porta código MIT de `gammons/slk` (auth de escritorio, WebSocket, mrkdwn, imágenes) con encabezado de atribución en cada archivo y en `THIRD_PARTY_NOTICES.md`.
- **Paridad UI/TUI:** toda operación de mensajería tiene que estar en los dos clientes. Es la condición central del proyecto.
- **El plugin QML corre sin sandbox** dentro de `omarchy-shell`: nada de tokens ni requests a Slack desde QML. Colores y fuentes salen sólo del singleton `Style`.
- **El tema de la TUI** sale de la plantilla `~/.config/omarchy/themed/omaslack.toml.tpl` y del hook `theme-set.d` que manda `SIGUSR1`.
