## ADDED Requirements

### Requirement: Manifest válido del plugin
El plugin SHALL tener un `manifest.json` con `schemaVersion: 1`, `id` = `ar.scabb.omaslack` (DNS invertido del dominio `scabb.ar`; es permanente en el marketplace y nunca puede usar `omarchy.*`), `name`, `version`, `author`, `license`, `description`, `kinds: ["service","bar-widget","panel"]`, `entryPoints` y el bloque `barWidget`. SHALL pasar `omarchy plugin validate` y no SHALL tener symlinks.

#### Scenario: Validación en CI
- **WHEN** corre el CI del repo
- **THEN** `omarchy plugin validate plugin/` y `qmllint` terminan sin errores

### Requirement: Instalación y desinstalación documentadas
El README del plugin SHALL explicar cómo instalar el binario (AUR) y el plugin (`omarchy plugin add <git-url> --enable`), cómo ubicarlo en la barra y cómo desinstalarlo por completo: plugin, servicio, plantilla, hook y datos.

#### Scenario: Desinstalar
- **WHEN** el usuario sigue los pasos de desinstalación
- **THEN** no quedan la unit de systemd, el hook, la plantilla ni las credenciales en el keyring

### Requirement: Plantilla de tema y hook
La instalación SHALL dejar `~/.config/omarchy/themed/omaslack.toml.tpl` (con las variables de color de Omarchy) y el hook `~/.config/omarchy/hooks/theme-set.d/omaslack`, que manda `SIGUSR1` a las TUIs abiertas. No SHALL pisar archivos que el usuario haya modificado.

#### Scenario: Plantilla editada por el usuario
- **WHEN** ya existe un `omaslack.toml.tpl` con cambios del usuario
- **THEN** la instalación no lo sobrescribe y avisa que hay una versión nueva disponible

### Requirement: Atajos y launcher
El proyecto SHALL incluir un `.desktop` para la TUI (visible en el launcher de Omarchy) y SHALL documentar los atajos sugeridos para Hyprland: abrir o cerrar la ventana (`omarchy-shell shell toggle ar.scabb.omaslack`) y abrir o enfocar la TUI.

#### Scenario: Buscar en el launcher
- **WHEN** el usuario busca "omaslack" en el launcher
- **THEN** aparece la entrada que abre o enfoca la TUI

### Requirement: Diagnóstico
`omaslack doctor` SHALL verificar y reportar: versión del daemon y del protocolo, estado del servicio, permisos del socket, dónde están las credenciales, proveedores por workspace (con el aviso de términos de uso para `desktop-session`), presencia de la plantilla y el hook, y si el plugin está instalado y habilitado (`omarchy plugin list --json`).

#### Scenario: Falta el hook
- **WHEN** el hook `theme-set.d/omaslack` no existe
- **THEN** `doctor` lo marca como advertencia e indica el comando para reinstalarlo

### Requirement: Coexistencia con otros clientes de Slack
La documentación SHALL indicar cómo convivir con la app oficial (ocultar su ícono de bandeja, evitar notificaciones duplicadas) y con otros plugins de Slack del marketplace. El plugin no SHALL modificar la configuración de otros plugins.

#### Scenario: App oficial abierta
- **WHEN** la app oficial de Slack también está corriendo
- **THEN** el README explica cómo desactivar sus notificaciones para no recibirlas dos veces

### Requirement: Distribución
El binario SHALL publicarse con releases firmadas (goreleaser) y un paquete AUR `omaslack-bin`. El plugin SHALL enviarse al marketplace de Omarchy siguiendo `SUBMISSION.md`, declarando la instalación manual del binario si el marketplace lo exige.

#### Scenario: Instalación limpia
- **WHEN** un usuario nuevo instala `omaslack-bin` y después el plugin
- **THEN** después de `omaslack auth add` tiene la barra, el panel, la ventana y la TUI funcionando contra el mismo daemon
