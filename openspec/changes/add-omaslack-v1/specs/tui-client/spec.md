## ADDED Requirements

### Requirement: Cliente de terminal sobre el daemon
`omaslack tui` SHALL ser un cliente del daemon, sin conexión directa con Slack ni credenciales propias, y SHALL implementar todas las operaciones de la capacidad `messaging`.

#### Scenario: Sin credenciales en la TUI
- **WHEN** se inspecciona el proceso `omaslack tui`
- **THEN** no tiene abierto ningún archivo de credenciales ni conexiones de red a Slack, sólo el socket del daemon

### Requirement: Layout de paneles
La TUI SHALL mostrar un rail de workspaces, una sidebar de conversaciones, el panel de mensajes, un panel de hilo opcional, el compose y una barra de estado con el modo, la conversación, el workspace y el estado de conexión. La sidebar SHALL poder ocultarse y cambiar de ancho.

#### Scenario: Terminal angosta
- **WHEN** la terminal tiene menos de 80 columnas
- **THEN** la sidebar se oculta automáticamente y se puede mostrar con un atajo

### Requirement: Modos y teclas tipo vim
La TUI SHALL tener los modos NORMAL, INSERT y COMMAND, con `j/k` para moverse, `h/l`/`Tab` para cambiar de panel, `i` para escribir, `Esc` para volver, `1`–`9` para cambiar de workspace y `Ctrl+k`/`Ctrl+t` para el buscador. Las teclas SHALL documentarse en una ayuda integrada (`?`).

#### Scenario: Cambiar de workspace
- **WHEN** el usuario aprieta `2` en modo NORMAL
- **THEN** la TUI pasa al segundo workspace del rail

### Requirement: Tema de Omarchy con recarga en caliente
La TUI SHALL leer el tema de `~/.local/state/omarchy/current/theme/omaslack.toml` (generado desde la plantilla de Omarchy). Si no existe, SHALL leer `colors.toml` del tema activo. Al recibir `SIGUSR1`, SHALL recargar el tema sin reiniciarse.

#### Scenario: Cambio de tema
- **WHEN** el usuario cambia el tema de Omarchy con la TUI abierta
- **THEN** el hook manda `SIGUSR1` y la TUI se redibuja con los colores nuevos sin perder el estado

### Requirement: Imágenes en la terminal
La TUI SHALL mostrar imágenes inline con el protocolo gráfico de kitty o con sixel, según lo que soporte la terminal, y SHALL usar bloques half-block como fallback. SHALL permitir abrir la imagen a tamaño completo en un visor externo.

#### Scenario: Terminal sin gráficos
- **WHEN** la TUI corre en Alacritty
- **THEN** las imágenes se muestran con half-block y se pueden abrir en el visor externo con un atajo

### Requirement: Lanzamiento al estilo Omarchy
La TUI SHALL poder lanzarse con `omarchy-launch-or-focus-tui omaslack tui` y con una clase de ventana fija (`org.omarchy.omaslack`), para que se puedan escribir reglas de Hyprland y atajos.

#### Scenario: Atajo con la TUI ya abierta
- **WHEN** la TUI ya está abierta y el usuario aprieta el atajo
- **THEN** se enfoca la ventana existente en lugar de abrir otra
