## ADDED Requirements

### Requirement: Notificaciones de escritorio centralizadas
Las notificaciones SHALL emitirse desde el daemon, una sola vez por mensaje, aunque haya varios clientes abiertos. Las SHALL emitir por el servicio de notificaciones del escritorio (Freedesktop, que en Omarchy atiende `omarchy.notifications`).

#### Scenario: TUI y ventana abiertas
- **WHEN** llega un DM con la TUI y la ventana de la UI abiertas
- **THEN** se muestra una sola notificación

### Requirement: Reglas de notificación
Por defecto SHALL notificarse DMs, DMs grupales, menciones (directas y de grupos del usuario) y respuestas en hilos que el usuario sigue. El usuario SHALL poder agregar palabras clave y canales que notifiquen todo, y silenciar canales. No SHALL notificarse lo que el usuario envía ni lo que ya estaba pendiente cuando arrancó el daemon.

#### Scenario: Palabra clave
- **WHEN** el usuario configuró la palabra clave "deploy" y un mensaje en un canal la contiene
- **THEN** se emite una notificación aunque el canal no esté en la lista de canales que notifican todo

### Requirement: Supresión si la conversación está a la vista
El daemon SHALL omitir la notificación cuando algún cliente reporta que esa conversación está abierta y con foco.

#### Scenario: Conversación abierta en la ventana con foco
- **WHEN** llega un mensaje a la conversación abierta en la ventana y la ventana tiene foco
- **THEN** no se emite ninguna notificación

### Requirement: Clic en la notificación
Al hacer clic en una notificación, SHALL abrirse la conversación (o el hilo) en el cliente preferido: la ventana de la UI si el plugin está cargado y, si no, la TUI con `omarchy-launch-or-focus-tui`. La preferencia SHALL poder configurarse.

#### Scenario: Preferencia UI con el plugin cargado
- **WHEN** el usuario hace clic en la notificación de un DM y su preferencia es UI
- **THEN** se abre la ventana del plugin en ese DM, mediante el evento `ui.focus`

### Requirement: No molestar
El usuario SHALL poder activar No molestar en Slack por un tiempo (30 min, 1 h, 2 h, hasta mañana) y terminarlo, desde la UI (barra o panel) y desde la TUI. Mientras esté activo, el daemon no SHALL emitir notificaciones y la barra SHALL indicarlo.

#### Scenario: Snooze desde la barra
- **WHEN** el usuario activa No molestar por 1 hora desde el panel
- **THEN** Slack queda en DND 1 hora, el ícono de la barra muestra el indicador de silencio y la TUI muestra el mismo estado

### Requirement: Presencia
El usuario SHALL poder poner su presencia en automática o ausente desde los dos clientes, y los clientes SHALL mostrar la presencia de las personas en DMs cuando el proveedor lo soporte.

#### Scenario: Ponerse ausente desde la TUI
- **WHEN** el usuario corre el comando `:away` en la TUI
- **THEN** la presencia en Slack pasa a ausente y la UI lo refleja
