## ADDED Requirements

### Requirement: Recepción en tiempo real
El sync engine SHALL consumir el stream de eventos del proveedor, normalizarlo a eventos de dominio (`message.created`, `message.updated`, `message.deleted`, `reaction.changed`, `conversation.updated`, `read.changed`, `presence.changed`, `typing`), persistirlos en el Store y publicarlos a los clientes suscriptos.

#### Scenario: Llega un mensaje nuevo
- **WHEN** alguien publica un mensaje en un canal del que el usuario es miembro
- **THEN** el mensaje queda guardado en el Store y los clientes suscriptos reciben `message.created` en menos de 1 segundo desde que llegó el evento del proveedor

### Requirement: Reconexión con backoff
Ante una desconexión, el engine SHALL reintentar con backoff exponencial con jitter, entre 1 y 30 segundos, y publicar `connection.state` (`connecting`, `connected`, `disconnected`) en cada transición.

#### Scenario: Se cae la red
- **WHEN** se pierde la conexión de red durante 2 minutos
- **THEN** los clientes ven el estado `disconnected` y después `connecting`, y al volver la red el estado pasa a `connected` sin intervención del usuario

### Requirement: Reconciliación tras reconectar
Después de reconectar, el engine SHALL traer lo que pasó durante el corte en las conversaciones con actividad (como mínimo las abiertas en algún cliente y las que tienen menciones o DMs) y actualizar los contadores de no leídos.

#### Scenario: Mensajes durante el corte
- **WHEN** llegaron 3 DMs mientras el daemon estaba desconectado
- **THEN** después de reconectar los 3 mensajes aparecen en el Store y el contador de DMs refleja 3 no leídos

### Requirement: Cache local en SQLite
El Store SHALL ser una base SQLite por workspace, en modo WAL, con migraciones versionadas. SHALL tratarse como cache: borrarla no MUST perder datos del usuario, salvo borradores no enviados y preferencias locales.

#### Scenario: Arranque en frío con cache
- **WHEN** el daemon arranca y ya existe una cache de la sesión anterior
- **THEN** los clientes pueden mostrar la sidebar y los últimos mensajes desde la cache antes de que termine la reconexión

#### Scenario: Cache borrada
- **WHEN** el usuario borra el directorio de la cache y reinicia el daemon
- **THEN** el daemon reconstruye la cache desde Slack sin pedir credenciales de nuevo

### Requirement: Backfill bajo demanda
El historial de una conversación SHALL traerse por páginas cuando un cliente la abre o pide mensajes más viejos, en lugar de descargar todo el workspace al arrancar.

#### Scenario: Scroll hacia atrás
- **WHEN** el usuario llega al principio de los mensajes cargados de una conversación
- **THEN** el daemon trae la página anterior y el cliente la agrega sin perder la posición del scroll

### Requirement: Presupuesto de rate limit
El engine SHALL mantener un presupuesto por método y tier de la API, respetar `Retry-After` y priorizar lo que pide el usuario (abrir una conversación, enviar) sobre las tareas de fondo (backfill, presencia). Cuando el proveedor es una app que no está en el Marketplace ni es interna, SHALL aplicar el límite de 1 llamada por minuto a `conversations.history` y `conversations.replies`.

#### Scenario: Pedido del usuario con el presupuesto agotado por backfill
- **WHEN** hay backfill en curso y el usuario abre una conversación
- **THEN** el pedido del usuario pasa adelante en la cola y el backfill se pausa hasta que vuelva a haber presupuesto

#### Scenario: Slack responde 429
- **WHEN** Slack responde HTTP 429 con `Retry-After: 30`
- **THEN** no se hacen llamadas a ese método durante 30 segundos y los clientes reciben un evento `connection.state` con el detalle `rate_limited`
