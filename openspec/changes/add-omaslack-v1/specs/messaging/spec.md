## ADDED Requirements

### Requirement: Paridad entre UI y TUI
Toda operación de mensajería de esta capacidad SHALL estar disponible tanto en la TUI como en la UI (ventana del plugin). Que un cliente no ofrezca una operación sólo SHALL pasar cuando el proveedor no la soporta.

#### Scenario: Operación hecha en un cliente se ve en el otro
- **WHEN** el usuario reacciona a un mensaje desde la TUI mientras la ventana de la UI está abierta en la misma conversación
- **THEN** la reacción aparece en la ventana sin recargar, por el evento `reaction.changed`

### Requirement: Listado de conversaciones
Los clientes SHALL mostrar canales, DMs y DMs grupales del workspace activo, agrupados por las secciones de Slack cuando el proveedor las soporte, con marcas de no leído y contadores de menciones.

#### Scenario: Canal silenciado
- **WHEN** un canal está silenciado en Slack y recibe mensajes
- **THEN** aparece como no leído atenuado, sin sumar al badge de la barra

### Requirement: Lectura de mensajes
Los clientes SHALL mostrar los mensajes con autor, hora, separadores de día, mrkdwn renderizado (negrita, itálica, tachado, código, bloques, citas, links), menciones resueltas a nombres, emoji, indicador de editado, respuestas en hilo y reacciones.

#### Scenario: Mensaje con mención y link
- **WHEN** un mensaje contiene `<@U123>` y `<https://x.y|texto>`
- **THEN** se muestra `@NombreVisible` y el link con el texto "texto", que se puede abrir con `xdg-open`

### Requirement: Envío, edición y borrado
El usuario SHALL poder enviar mensajes de varias líneas, editar y borrar los propios (borrar pide confirmación) y responder en hilo, con la opción de enviar también al canal.

#### Scenario: Borrar un mensaje propio
- **WHEN** el usuario elige borrar un mensaje propio y confirma
- **THEN** el mensaje desaparece en todos los clientes por `message.deleted`

### Requirement: Hilos
Los clientes SHALL abrir un hilo en una vista propia (panel lateral en la TUI y vista o panel en la ventana de la UI), marcar los hilos con respuestas nuevas y ofrecer una vista de "hilos que sigo".

#### Scenario: Respuesta nueva en un hilo seguido
- **WHEN** llega una respuesta a un hilo que el usuario sigue
- **THEN** el mensaje raíz muestra el indicador de "nuevo" y el hilo aparece en la vista de hilos

### Requirement: Reacciones
El usuario SHALL poder agregar y quitar reacciones con un selector de emoji que ofrezca primero los usados recientemente, y ver quién reaccionó.

#### Scenario: Quitar la reacción propia
- **WHEN** el usuario elige una reacción que ya había puesto
- **THEN** la reacción se quita y el contador baja en uno

### Requirement: Archivos e imágenes
El usuario SHALL poder adjuntar archivos (rutas o imagen del portapapeles) con un texto, y abrir o descargar los archivos recibidos. Las descargas SHALL pasar por el daemon (`files.fetch`) y los clientes sólo SHALL recibir la ruta local.

#### Scenario: Pegar una imagen del portapapeles
- **WHEN** el usuario pega una imagen en el compose y envía
- **THEN** la imagen se sube a la conversación con el texto escrito, en un solo mensaje

### Requirement: Menciones y autocompletado
El compose SHALL autocompletar `@usuario`, `#canal` y `:emoji:`, y traducir las menciones al formato de Slack al enviar.

#### Scenario: Autocompletar usuario
- **WHEN** el usuario escribe `@ana` y elige "Ana Pérez" del autocompletado
- **THEN** el mensaje enviado contiene `<@ID_DE_ANA>`

### Requirement: Búsqueda y saltos
Los clientes SHALL ofrecer un buscador de conversaciones y personas (fuzzy, abre DMs y permite unirse a canales) y, cuando el proveedor lo soporte, la búsqueda de mensajes de Slack. Un resultado de búsqueda SHALL abrir la conversación en ese mensaje.

#### Scenario: Resultado de búsqueda
- **WHEN** el usuario elige un resultado de búsqueda de un mensaje de hace un mes
- **THEN** la conversación se abre posicionada en ese mensaje y no en el final

### Requirement: Estado de lectura único
Marcar una conversación como leída en cualquier cliente SHALL actualizar el Store del daemon, enviarse a Slack y publicar `read.changed` y `unread.summary`, de modo que todos los clientes queden de acuerdo en menos de 1 segundo.

#### Scenario: Leer en la ventana, ver la barra
- **WHEN** el usuario abre en la ventana un DM no leído
- **THEN** el badge de la barra baja en menos de 1 segundo, sin esperar ningún polling

### Requirement: Borradores compartidos
Los borradores SHALL guardarse en el daemon por conversación e hilo, y SHALL estar disponibles en cualquier cliente.

#### Scenario: Empezar en la ventana, terminar en la TUI
- **WHEN** el usuario escribe un borrador en la ventana, la cierra y abre la misma conversación en la TUI
- **THEN** el compose de la TUI muestra el mismo borrador
