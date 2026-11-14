# device-hub

Hub de periféricos: un daemon en Go lee la batería de los dispositivos y un
panel de Quickshell los muestra. El icono del systray (SNI) abre y cierra el
panel con un click, así que funciona con cualquier barra compatible con SNI.

## Arquitectura

```
device-hubd (Go)                    Quickshell (QML)
  ├─ providers: razer, g435, fantech   panel.qml
  ├─ store: cache + state.json   ───►  FileView (estado)
  ├─ tray: SNI + icono          ───►  qs ipc call devicehub toggle
  └─ dbus: org.devicehub
```

- **Estado**: el daemon escribe `$XDG_RUNTIME_DIR/device-hub/state.json` y el
  panel lo observa con `Quickshell.Io.FileView` (`watchChanges`).
- **Toggle**: el click del SNI ejecuta `qs ipc -p <panel.qml> call devicehub
  toggle`, que alterna la visibilidad al instante (sin polling).
- **D-Bus** (`org.devicehub`): `GetDevices()` y la señal `Toggled`, para otras
  herramientas. Quickshell 0.3.x no tiene cliente D-Bus genérico; por eso el
  panel no lo consume.
- **Panel standalone**: corre como su propia instancia de Quickshell
  (`qs -p panel/panel.qml`), no como plugin de la barra de Ryoku. Así una
  actualización de Ryoku no puede romperlo.
- **Cierre**: la superficie es una capa Overlay fullscreen transparente; el área
  libre es el catcher de click-afuera, y la superficie toma foco de teclado para
  cerrar con Escape. Mismo enfoque que los menús de Ryoku.

## Dispositivos

| Dispositivo | Transporte | Lectura |
| --- | --- | --- |
| Razer Naga Pro | sysfs (`razermouse`) | `charge_level` 0-255 → % |
| Logitech G435 | hidraw (Holtek) | voltaje → curva litio |
| Fantech Atom HE68 PRO | hidraw (protocolo propio) | BCD en byte 11 |

Razer necesita el grupo `openrazer` y `systemctl --user start openrazer-daemon`.
Leer el G435 corta el audio ~1 s: el daemon cachea y refresca cada `-interval`.

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
systemctl --user status device-hub.service device-hub-panel.service
systemctl --user restart device-hub.service
journalctl --user -u device-hub-panel.service -f
```

Los units viven en `~/.config/systemd/user/` (`device-hub.service` y
`device-hub-panel.service`), habilitados en `default.target` como el tray viejo.

## Tests

```sh
go test ./...
go vet ./...
```

## Roadmap

- [x] Instalar daemon + panel como servicios de usuario.
- [x] Cerrar con click fuera y con Escape.
- [ ] RGB vía OpenRGB (SDK TCP 6742) + sincronización.
- [ ] OpenRazer completo (DPI, macros) y más dispositivos.
