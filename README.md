# device-hub

Hub de periféricos: un daemon en Go lee la batería de los dispositivos y
controla su RGB, y un panel de Quickshell los muestra. El icono del systray
(SNI) abre y cierra el panel con un click, así que funciona con cualquier barra
compatible con SNI.

## Arquitectura

```
device-hubd (Go)                        Quickshell (QML)
  ├─ providers: razer, g435, fantech       panel.qml
  ├─ store: cache + state.json     ───►    FileView (estado)
  ├─ rgb: manager + control.sock   ◄───    Socket (comandos de color)
  ├─ tray: SNI + icono             ───►    qs ipc call devicehub toggle
  └─ dbus: org.devicehub
```

- **Estado**: el daemon escribe `$XDG_RUNTIME_DIR/device-hub/state.json` y el
  panel lo observa con `Quickshell.Io.FileView` (`watchChanges`). El archivo se
  reescribe en sitio: `atomicWrites: false` y el parseo ocurre en `onLoaded`,
  después de `reload()`.
- **Comandos**: el panel manda JSON por línea a
  `$XDG_RUNTIME_DIR/device-hub/control.sock` con `Quickshell.Io.Socket`.
- **Toggle**: el click del SNI ejecuta `qs ipc -p <panel.qml> call devicehub
  toggle`, que alterna la visibilidad al instante (sin polling).
- **D-Bus** (`org.devicehub`): `GetDevices()`, `SetRGB()`, `SyncRGB()` y las
  señales `Toggled` / `RGBChanged`, para otras herramientas. Quickshell 0.3.x no
  tiene cliente D-Bus genérico; por eso el panel no lo consume.
- **Panel standalone**: corre como su propia instancia de Quickshell
  (`qs -p panel/panel.qml`), no como plugin de la barra de Ryoku. Así una
  actualización de Ryoku no puede romperlo.
- **Cierre**: la superficie es una capa Overlay fullscreen transparente; el área
  libre es el catcher de click-afuera, y la superficie toma foco de teclado para
  cerrar con Escape. Mismo enfoque que los menús de Ryoku.

## Dispositivos

| Dispositivo | Transporte | Lectura | RGB |
| --- | --- | --- | --- |
| Razer Naga Pro | sysfs (`razermouse`) | `charge_level` 0-255 → % | OpenRGB SDK (3 zonas) |
| Logitech G435 | hidraw (Holtek) | voltaje → curva litio | — |
| Fantech Atom HE68 PRO | hidraw (protocolo propio) | BCD en byte 11 | hidraw directo (128 LEDs) |

Razer necesita el grupo `openrazer` y `systemctl --user start openrazer-daemon`.
Leer el G435 corta el audio ~1 s: el daemon cachea y refresca cada `-interval`.

## RGB

Dos backends, unificados por `device.RGBDevice` (`Zones`, `SetZone`, `SetAll`):

- **Razer → OpenRGB SDK** (TCP `127.0.0.1:6742`, protocolo binario `ORGB`).
  openrazer expone solo batería, así que el color va por el servidor de OpenRGB:
  negociar versión, pedir controladores, `SET_CUSTOM_MODE` y `UPDATE_LEDS`. El
  Naga Pro se ubica por nombre (`naga pro`). Se usa el SDK y no el CLI
  `openrgb --client` porque el CLI duplica dispositivos y vuelve ambiguo el
  índice.
- **Fantech → receptor 2.4 GHz** (hidraw, usage page `0xFF60`, reports de 32
  bytes sin report ID). OpenRGB no detecta el teclado: se activa el modo custom
  (`AA 23 10 …`) y se manda la tabla de 128 LEDs en 22 reports de seis LEDs,
  ~26 ms entre cada uno.

El daemon mantiene el último color en `state.json` (`rgb` por dispositivo y
`sync`), y el panel pinta un punto con ese color en cada card. La sección RGB
tiene chips de destino (`Todos` o un dispositivo) y una paleta de 8 colores.

Requisito: el servidor de OpenRGB tiene que estar escuchando en 6742.

```sh
systemctl --user enable --now openrgb-server.service   # openrgb --server --noautoconnect
```

API de control (socket Unix, JSON por línea; `#RRGGBB`):

```sh
printf '{"action":"sync","color":"#7aa2f7"}\n' | socat -t 3 - UNIX-CONNECT:$XDG_RUNTIME_DIR/device-hub/control.sock
printf '{"action":"set","device":"razer.naga-pro","color":"#ff4d4d"}\n' | socat -t 3 - UNIX-CONNECT:$XDG_RUNTIME_DIR/device-hub/control.sock
```

`socat` necesita `-t 3` (o similar): con el timeout por defecto cierra antes de
leer la respuesta y el comando puede perderse.

Equivalente por D-Bus:

```sh
busctl --user call org.devicehub /org/devicehub org.devicehub.Manager SyncRGB yyy 122 162 247
busctl --user call org.devicehub /org/devicehub org.devicehub.Manager SetRGB syyy "razer.naga-pro" 255 77 77
```

Tests de integración (requieren el servidor y cambian el color real):

```sh
OPENRGB_ADDR=127.0.0.1:6742 go test -run TestLiveControllers -v ./internal/rgb
OPENRGB_ADDR=127.0.0.1:6742 OPENRGB_SET_COLOR=00ff00 go test -run TestLiveSetColor -v ./internal/rgb
```

## Uso

El Go del sistema está compilado para x86-64-v4; usá el Go oficial:

```sh
export PATH=$HOME/.local/go/bin:$PATH
go build -o device-hubd ./cmd/device-hubd
```

Daemon (detecta `panel/panel.qml` junto al binario o en el cwd):

```sh
./device-hubd -interval 60s
```

Panel aparte:

```sh
qs -p panel/panel.qml
```

Flags del daemon:

- `-interval` — intervalo de refresco (default `60s`).
- `-panel` — ruta a `panel.qml`; vacío desactiva el panel.
- `-ipc-target` — target IPC del panel (default `devicehub`).

## Servicios de usuario

```sh
systemctl --user status device-hub.service device-hub-panel.service openrgb-server.service
systemctl --user restart device-hub.service
journalctl --user -u device-hub-panel.service -f
```

Los units viven en `~/.config/systemd/user/` (`device-hub.service`,
`device-hub-panel.service` y `openrgb-server.service`), habilitados en
`default.target` como el tray viejo. `openrgb-server.service` es el backend RGB;
sin él, los cambios de color del Razer fallan con "connection refused".

## Tests

```sh
go test ./...
go vet ./...
```

## Roadmap

- [x] Instalar daemon + panel como servicios de usuario.
- [x] Cerrar con click fuera y con Escape.
- [x] RGB vía OpenRGB (SDK TCP 6742) + sincronización.
- [ ] OpenRazer completo (DPI, macros) y más dispositivos.
