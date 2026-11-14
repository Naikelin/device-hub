# device-hub

Hub de periféricos: un daemon en Go lee la batería de los dispositivos y
controla sus LEDs, y un panel de Quickshell los muestra. El icono del systray
(SNI) abre y cierra el panel con un click, así que funciona con cualquier barra
compatible con SNI.

## Arquitectura

```
device-hubd (Go)                        Quickshell (QML)
  ├─ providers: razer, g435, fantech       panel.qml  (batería + controladores)
  ├─ providers/openrgb: controladores      rgb.qml    (solo color)
  ├─ led: elección por dispositivo  ─────►  FileView (state.json)
  │     ├─ openrgb  (SDK 6742)              Socket (comandos)
  │     ├─ openrazer (D-Bus)
  │     └─ directo   (hidraw)
  ├─ store: cache + state.json     ───►    FileView (estado)
  ├─ control: socket JSON          ◄───    Socket
  ├─ tray: SNI + icono             ───►    qs ipc call devicehub toggle
  └─ dbus: org.devicehub
```

- **Estado**: el daemon escribe `$XDG_RUNTIME_DIR/device-hub/state.json` y el
  panel lo observa con `Quickshell.Io.FileView` (`watchChanges`). El archivo se
  reescribe en sitio: `atomicWrites: false` y el parseo ocurre en `onLoaded`,
  después de `reload()`.
- **Comandos**: el panel y la ventana RGB mandan JSON por línea a
  `$XDG_RUNTIME_DIR/device-hub/control.sock` con `Quickshell.Io.Socket`.
- **Toggle**: el click del SNI ejecuta `qs ipc -p <panel.qml> call devicehub
  toggle`, que alterna la visibilidad al instante (sin polling).
- **D-Bus** (`org.devicehub`): `GetDevices()`, `SetRGB()`, `SyncRGB()`,
  `SetLED()`, `SetController()` y las señales `Toggled` / `RGBChanged`.
- **Panel standalone**: el hub corre como su propia instancia de Quickshell
  (`qs -p panel/panel.qml`), no como plugin de la barra de Ryoku. Así una
  actualización de Ryoku no puede romperlo. La ventana RGB es otra instancia
  (`qs -p panel/rgb.qml`), flotante y bajo demanda.
- **Cierre del hub**: la superficie es una capa Overlay fullscreen transparente;
  el área libre es el catcher de click-afuera, y la superficie toma foco de
  teclado para cerrar con Escape. La ventana RGB cierra con Escape o con su `×`.

## Dispositivos

| Dispositivo | Transporte | Lectura | LEDs |
| --- | --- | --- | --- |
| Razer Naga Pro | sysfs (`razermouse`) | `charge_level` 0-255 → % | `openrazer` o `openrgb` |
| Logitech G435 | hidraw (Holtek) | voltaje → curva litio | sin LEDs |
| Fantech Atom HE68 PRO | hidraw (protocolo propio) | BCD en byte 11 | `directo` (128 LEDs) |
| Controladores de OpenRGB | SDK `ORGB` (TCP 6742) | — | `openrgb` |

La lista del hub muestra todos: los tres periféricos y los controladores que
detecta OpenRGB (motherboard, RAM, GPU, altavoz, micrófono). El G435 no tiene
LEDs, así que su card solo muestra batería.

Razer necesita el grupo `openrazer` y `systemctl --user start openrazer-daemon`.
Leer el G435 corta el audio ~1 s: el daemon cachea y refresca cada `-interval`.

## Control de LEDs

Cada dispositivo tiene un **controlador** (el backend que maneja sus LEDs) y
un flag **encendido**, persistidos en `~/.config/device-hub/leds.json`:

```json
{ "devices": { "razer.naga-pro": { "controller": "openrazer", "enabled": true } } }
```

- **openrazer** — D-Bus de openrazer
  (`razer.device.lighting.chroma.setStatic` / `setNone`). Es el nativo: mantiene
  el Naga sincronizado con el resto del ecosistema Razer.
- **openrgb** — servidor SDK (TCP `127.0.0.1:6742`, protocolo binario `ORGB`):
  negociar versión, pedir controladores, `SET_CUSTOM_MODE` y `UPDATE_LEDS`. El
  Naga se ubica por nombre (`naga pro`). Se usa el SDK y no el CLI
  `openrgb --client` porque el CLI duplica dispositivos y vuelve ambiguo el
  índice.
- **directo** — Fantech por hidraw (usage page `0xFF60`, reports de 32 bytes sin
  report ID). OpenRGB no detecta el teclado: se activa el modo custom
  (`AA 23 10 …`) y se manda la tabla de 128 LEDs en 22 reports de seis LEDs,
  ~26 ms entre cada uno.

Apagar un dispositivo (`off`) usa el backend elegido: `setNone` para openrazer,
negro para OpenRGB y una tabla negra para el Fantech. Al encenderlo se re-aplica
su último color. Cambiar de controlador re-aplica el estado actual con el
nuevo backend, así que el cambio se ve enseguida.

Al arrancar, el daemon re-apaga solo los dispositivos que quedaron apagados
(`leds.json`), para que el ahorro de batería del Fantech sobreviva un reinicio.
Los encendidos no se tocan: su color pertenece al perfil de login de OpenRGB.

El SDK de OpenRGB no tiene paquete para elegir un modo arbitrario (solo
`SET_CUSTOM_MODE`), así que el quirk Off→Static del Leviathan no se reproduce.

### Ventana RGB

El hub ya no cambia colores. La paleta y el hexágono viven en `panel/rgb.qml`,
una ventana flotante con chips de destino (`Todos` o un dispositivo), paleta de
8 colores, entrada hex y el estado de cada dispositivo. Se abre desde el botón
"Control RGB…" del hub o desde el menú del tray.

API de control (socket Unix, JSON por línea; `#RRGGBB`):

```sh
printf '{"action":"sync","color":"#7aa2f7"}\n' | socat -t 3 - UNIX-CONNECT:$XDG_RUNTIME_DIR/device-hub/control.sock
printf '{"action":"set","device":"razer.naga-pro","color":"#ff4d4d"}\n' | socat -t 3 - UNIX-CONNECT:$XDG_RUNTIME_DIR/device-hub/control.sock
printf '{"action":"off","device":"fantech.atom-he68-pro"}\n' | socat -t 3 - UNIX-CONNECT:$XDG_RUNTIME_DIR/device-hub/control.sock
printf '{"action":"controller","device":"razer.naga-pro","controller":"openrgb"}\n' | socat -t 3 - UNIX-CONNECT:$XDG_RUNTIME_DIR/device-hub/control.sock
printf '{"action":"window"}\n' | socat -t 3 - UNIX-CONNECT:$XDG_RUNTIME_DIR/device-hub/control.sock
```

Acciones: `set`, `sync`, `on`, `off`, `controller`, `window`.

`socat` necesita `-t 3` (o similar): con el timeout por defecto cierra antes de
leer la respuesta y el comando puede perderse.

Equivalente por D-Bus:

```sh
busctl --user call org.devicehub /org/devicehub org.devicehub.Manager SyncRGB yyy 122 162 247
busctl --user call org.devicehub /org/devicehub org.devicehub.Manager SetRGB syyy "razer.naga-pro" 255 77 77
busctl --user call org.devicehub /org/devicehub org.devicehub.Manager SetLED sb "fantech.atom-he68-pro" false
busctl --user call org.devicehub /org/devicehub org.devicehub.Manager SetController ss "razer.naga-pro" "openrgb"
```

Requisito: el servidor de OpenRGB tiene que estar escuchando en 6742.

```sh
systemctl --user enable --now openrgb-server.service   # openrgb --server --noautoconnect
```

El daemon espera a que termine de detectar (5-10 s) antes de registrar los
controladores. Si el servidor no está, los periféricos con batería igual
funcionan; los controladores de OpenRGB aparecen tras reiniciar el daemon.

## Uso

El Go del sistema está compilado para x86-64-v4; usá el Go oficial:

```sh
export PATH=$HOME/.local/go/bin:$PATH
go build -o device-hubd ./cmd/device-hubd
```

Daemon (detecta `panel/panel.qml` y `panel/rgb.qml` junto al binario o en el cwd):

```sh
./device-hubd -interval 60s
```

Panel aparte:

```sh
qs -p panel/panel.qml
qs -p panel/rgb.qml
```

Flags del daemon:

- `-interval` — intervalo de refresco (default `60s`).
- `-panel` — ruta a `panel.qml`; vacío desactiva el panel.
- `-rgb` — ruta a `rgb.qml`; vacío desactiva la ventana RGB.
- `-ipc-target` — target IPC del panel (default `devicehub`).
- `-rgb-ipc-target` — target IPC de la ventana RGB (default `devicehub-rgb`).

## Servicios de usuario

```sh
systemctl --user status device-hub.service device-hub-panel.service openrgb-server.service
systemctl --user restart device-hub.service
journalctl --user -u device-hub-panel.service -f
```

Los units viven en `~/.config/systemd/user/` (`device-hub.service`,
`device-hub-panel.service` y `openrgb-server.service`), habilitados en
`default.target` como el tray viejo. `device-hub.service` arranca después de
`openrgb-server.service` y lo requiere (`Wants`); `openrgb-server.service` es el
backend RGB, y sin él los cambios de color del Razer fallan con "connection
refused".

## Tests

```sh
go test ./...
go vet ./...
```

Cubren la elección y persistencia de LEDs, el estado JSON del panel, el
enrutado del socket de control y el slug/tipo de los controladores.

## Roadmap

- [x] Instalar daemon + panel como servicios de usuario.
- [x] Cerrar con click fuera y con Escape.
- [x] RGB vía OpenRGB (SDK TCP 6742) + sincronización.
- [x] Selector de controlador por dispositivo, apagado y ventana RGB aparte.
- [ ] OpenRazer completo (DPI, macros) y más dispositivos.
