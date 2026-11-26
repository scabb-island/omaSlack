## ADDED Requirements

### Requirement: Service único como cliente del daemon
El plugin SHALL tener un `Service.qml` (`kinds: service`) que mantenga la única conexión con el daemon y exponga modelos compartidos. `BarWidget`, `Panel` y la ventana SHALL leer de ese Service y no abrir conexiones propias.

#### Scenario: Barra y ventana coinciden
- **WHEN** la ventana marca una conversación como leída
- **THEN** el badge de la barra se actualiza con el mismo evento, sin ningún polling

### Requirement: Sin credenciales ni red en QML
El código QML no MUST leer credenciales ni hacer requests de red a Slack. Avatares, imágenes y archivos SHALL pedirse al daemon, que devuelve rutas locales.

#### Scenario: Revisión del plugin
- **WHEN** se buscan URLs `slack.com` o accesos al keyring en el código QML
- **THEN** no hay ninguno

### Requirement: Ícono de barra con badges
`BarWidget.qml` SHALL mostrar un ícono que siga el color de la barra, con un badge para DMs y menciones dirigidas al usuario y otro para canales y hilos. También SHALL mostrar el estado de conexión (conectado, reconectando, error, `auth_expired`) y DND, y un tooltip con el resumen por workspace.

#### Scenario: Daemon caído
- **WHEN** el daemon no responde
- **THEN** el ícono se atenúa y el tooltip dice cómo iniciarlo

#### Scenario: Clics
- **WHEN** el usuario hace clic izquierdo en el ícono
- **THEN** se abre el Panel de vista rápida. Con clic derecho, o eligiendo un elemento del panel, se abre la ventana completa

### Requirement: Panel de vista rápida
`Panel.qml` SHALL listar las conversaciones con no leídos (DMs y menciones primero) con una vista previa, y SHALL permitir responder rápido, marcar como leído, activar DND y abrir la conversación en la ventana o en la TUI.

#### Scenario: Respuesta rápida
- **WHEN** el usuario escribe una respuesta en el panel y aprieta Enter
- **THEN** el mensaje se envía y la conversación queda marcada como leída

### Requirement: Ventana completa
El plugin SHALL ofrecer una ventana (`FloatingWindow` de Quickshell, con un título fijo para las reglas de Hyprland) con sidebar de conversaciones y workspaces, mensajes, hilos, compose, reacciones, adjuntos, búsqueda y buscador. SHALL cubrir todas las operaciones de la capacidad `messaging`.

#### Scenario: Abrir por IPC
- **WHEN** se corre `omarchy-shell shell toggle ar.scabb.omaslack`
- **THEN** la ventana se muestra si estaba oculta y se oculta si estaba visible

### Requirement: Estilo de Omarchy
Toda la UI SHALL tomar colores, fuentes y espaciados sólo del singleton `Style` del shell, sin colores hardcodeados, y SHALL verse bien con temas claros y oscuros.

#### Scenario: Tema claro
- **WHEN** el usuario cambia a un tema claro de Omarchy
- **THEN** la barra, el panel y la ventana cambian sin reiniciar el shell y mantienen el contraste legible

### Requirement: Uso completo por teclado
El panel y la ventana SHALL poder usarse sin mouse: moverse entre conversaciones y mensajes, abrir hilos, reaccionar, buscar, escribir y cerrar con `Esc`.

#### Scenario: Responder en un hilo sin mouse
- **WHEN** el usuario selecciona un mensaje con las flechas, abre su hilo, escribe y envía
- **THEN** la respuesta queda publicada en el hilo sin haber usado el mouse

### Requirement: Mensajes largos sin degradar el shell
La lista de mensajes SHALL estar virtualizada (`ListView`) y paginada. El parseo de mrkdwn SHALL hacerse en el daemon, que entrega al QML texto enriquecido ya listo, para no bloquear el hilo del shell.

#### Scenario: Canal con mucho historial
- **WHEN** el usuario hace scroll por 2.000 mensajes de un canal
- **THEN** el shell sigue respondiendo (la barra y otras superficies no se traban)

### Requirement: Configuración desde el manifest
Las opciones del widget (badges, qué cuenta como no leído, cliente preferido para abrir, ocultar el ícono si no hay nada) SHALL declararse en `barWidget.schema[]` con valores en `barWidget.defaults`, para que se puedan editar desde la configuración del shell.

#### Scenario: Ocultar si no hay nada
- **WHEN** la opción "ocultar si no hay no leídos" está activa y no hay no leídos
- **THEN** el ícono no ocupa lugar en la barra
