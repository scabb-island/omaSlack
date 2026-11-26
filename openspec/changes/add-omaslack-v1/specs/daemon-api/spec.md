## ADDED Requirements

### Requirement: Socket local privado
El daemon SHALL escuchar en un unix socket en `$XDG_RUNTIME_DIR/omaslack.sock` con permisos 0600 y SHALL rechazar las conexiones cuyo UID (obtenido con `SO_PEERCRED`) no sea el del dueño del daemon.

#### Scenario: Otro usuario intenta conectarse
- **WHEN** un proceso de otro UID se conecta al socket
- **THEN** el daemon cierra la conexión sin procesar ningún mensaje y lo registra en el log

### Requirement: JSON-RPC 2.0 sobre NDJSON
El protocolo SHALL ser JSON-RPC 2.0, con un objeto JSON por línea (NDJSON). Los errores SHALL usar los códigos estándar de JSON-RPC y códigos propios desde `-32000` (por ejemplo `-32001 incompatible_protocol`, `-32002 unsupported_capability`, `-32003 auth_expired`).

#### Scenario: Método inexistente
- **WHEN** un cliente envía `{"jsonrpc":"2.0","id":1,"method":"foo.bar"}`
- **THEN** el daemon responde con el error `-32601 Method not found` y la conexión sigue abierta

### Requirement: Handshake y versión de protocolo
Todo cliente SHALL empezar con `hello{client, protocol}`. El daemon SHALL responder con su versión de protocolo, su versión de daemon, los workspaces y las capacidades. Si la versión mayor del protocolo no coincide, SHALL responder `-32001` y cerrar la conexión.

#### Scenario: Plugin QML más nuevo que el daemon
- **WHEN** el plugin envía `hello{protocol: 2}` y el daemon soporta sólo la versión 1
- **THEN** el daemon responde `-32001 incompatible_protocol` y la barra muestra "actualizá omaslack"

### Requirement: Suscripción a eventos
Un cliente SHALL poder llamar a `events.subscribe{topics[]}` y, desde ese momento, recibir notificaciones JSON-RPC (sin `id`) de los tópicos pedidos en la misma conexión. `unread.summary` SHALL coalescerse a no más de una emisión cada 250 ms.

#### Scenario: Ráfaga de mensajes
- **WHEN** llegan 40 mensajes en un segundo
- **THEN** cada cliente suscripto a `message.created` recibe 40 eventos, y el suscripto a `unread.summary` recibe como máximo 4

### Requirement: Métodos de mensajería y estado
El daemon SHALL exponer, como mínimo, los métodos v1 listados en `design.md`: workspaces, conversations, messages, reactions, files, drafts, search, users, presence, dnd, unread.summary y ui.focus. Un método que requiere una capacidad que el proveedor no tiene SHALL responder `-32002 unsupported_capability`.

#### Scenario: Búsqueda sin soporte
- **WHEN** un cliente llama a `search.messages` en un workspace cuyo proveedor declara `search = false`
- **THEN** el daemon responde `-32002 unsupported_capability`

### Requirement: Servicio de usuario y arranque bajo demanda
El daemon SHALL distribuirse con una unit de systemd de usuario (`omaslack.service`). Si un cliente no encuentra el socket, SHALL intentar `systemctl --user start omaslack` antes de fallar.

#### Scenario: Se abre la TUI con el daemon apagado
- **WHEN** el usuario corre `omaslack tui` y el daemon no está corriendo
- **THEN** la TUI arranca el servicio, espera el socket hasta 5 segundos y se conecta

### Requirement: CLI de control para scripts
`omaslack ctl <método> [params-json]` SHALL hacer una llamada RPC e imprimir la respuesta como JSON. `omaslack ctl --stream <tópicos>` SHALL imprimir eventos como NDJSON en stdout y aceptar requests NDJSON por stdin, para que sirva de puente con clientes que no pueden abrir el socket directamente.

#### Scenario: Script de la barra
- **WHEN** se corre `omaslack ctl unread.summary`
- **THEN** stdout tiene un solo objeto JSON con `dms`, `mentions`, `channels` y `threads` por workspace y en total
