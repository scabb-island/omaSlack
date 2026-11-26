## ADDED Requirements

### Requirement: Proveedores de conexión intercambiables
El daemon SHALL hablar con Slack sólo a través de la interfaz `Provider`. En v1 SHALL existir dos implementaciones: `desktop-session` e `internal-app`. Cada workspace SHALL tener exactamente un proveedor asignado.

#### Scenario: Workspace con proveedor internal-app
- **WHEN** el usuario corre `omaslack auth add --provider internal-app` y carga un user token xoxp y un app token xapp válidos
- **THEN** el workspace queda registrado con `provider = internal-app` y el daemon se conecta usando la Web API y Socket Mode

#### Scenario: Workspace con proveedor desktop-session
- **WHEN** el usuario corre `omaslack auth add --provider desktop-session` y tiene la app de escritorio de Slack con la sesión iniciada
- **THEN** el CLI lista los workspaces encontrados en la sesión de escritorio, el usuario elige cuáles agregar y cada uno queda registrado con `provider = desktop-session`

### Requirement: Consentimiento explícito para el protocolo no oficial
El proveedor `desktop-session` MUST requerir que el usuario acepte un aviso que explica que usa un protocolo no oficial que puede violar los términos de uso de Slack. Sin esa aceptación, el workspace no MUST registrarse.

#### Scenario: El usuario rechaza el aviso
- **WHEN** el usuario elige `desktop-session` y responde "no" al aviso de términos de uso
- **THEN** no se guarda ninguna credencial y el CLI sugiere el proveedor `internal-app` con un link a su guía

### Requirement: Login alternativo por sesión del navegador
El proveedor `desktop-session` SHALL aceptar como alternativa un comando cURL copiado de las DevTools del navegador (un request a `/api/` de app.slack.com) y extraer de ahí el token xoxc y la cookie xoxd.

#### Scenario: No hay app de escritorio instalada
- **WHEN** no se encuentra la sesión de la app de escritorio y el usuario pega un cURL válido
- **THEN** el workspace queda registrado con las credenciales extraídas del cURL

### Requirement: Anuncio de capacidades del proveedor
Cada proveedor SHALL declarar un conjunto `Capabilities` (por ejemplo `realtime`, `search`, `files`, `presenceWrite`, `typing`, `customEmoji`). El daemon SHALL exponerlo por RPC para que la TUI y la UI oculten o deshabiliten lo que el proveedor no soporta.

#### Scenario: Función no soportada
- **WHEN** un proveedor declara `typing = false`
- **THEN** ni la TUI ni la UI muestran indicadores de "está escribiendo" para ese workspace

### Requirement: Credenciales en el keyring
Las credenciales SHALL guardarse en el Secret Service con el atributo `service=omaslack`. Si no hay Secret Service, SHALL usarse un archivo con permisos 0600 en `$XDG_CONFIG_HOME/omaslack/credentials` y mostrarse un aviso en `omaslack doctor`. Ningún método RPC MUST devolver credenciales.

#### Scenario: Sin Secret Service
- **WHEN** no hay ningún proveedor de Secret Service en D-Bus
- **THEN** las credenciales se guardan en el archivo con permisos 0600 y `omaslack doctor` marca el aviso "credenciales en archivo"

#### Scenario: Un cliente pide datos del workspace
- **WHEN** un cliente llama a `workspaces.list`
- **THEN** la respuesta incluye nombre, team_id, proveedor y estado, pero ningún token ni cookie

### Requirement: Multi-workspace en paralelo
El daemon SHALL mantener conectados en paralelo todos los workspaces registrados, cada uno con su propio proveedor, estado de conexión y contadores de no leídos.

#### Scenario: Dos workspaces con proveedores distintos
- **WHEN** hay un workspace con `internal-app` y otro con `desktop-session`
- **THEN** los dos reciben mensajes en tiempo real y `unread.summary` agrega los contadores de ambos

### Requirement: Credenciales vencidas
Cuando Slack rechace una credencial (por ejemplo `invalid_auth` o `token_revoked`), el daemon SHALL marcar el workspace como `auth_expired`, dejar de reintentar y publicar `connection.state` para que los clientes pidan volver a autenticar.

#### Scenario: Token revocado
- **WHEN** Slack responde `token_revoked` a un workspace
- **THEN** el workspace pasa a `auth_expired`, la barra muestra el ícono en estado de error y la TUI muestra "volver a autenticar: omaslack auth add"
