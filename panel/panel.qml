import QtQuick
import Quickshell
import Quickshell.Io
import Quickshell.Wayland

// Device Hub panel: one card per peripheral with battery, LED controller and
// LED on/off, plus the OpenRGB profile selector and a shortcut to the official
// GUI. The optional quick palette lives in rgb.qml (tray -> "Colores LED…").
//
// Battery and LED state come from the daemon through a JSON file (Quickshell
// 0.3.x has no generic D-Bus client). Show/hide is driven by IPC so the tray
// click feels instant:
//
//   qs ipc -p <this file> call devicehub toggle
//
// Commands go back to the daemon over a Unix socket, one JSON line per
// command. The window is a fullscreen transparent Overlay layer so the empty
// area is the outside-click catcher and the surface can take keyboard focus
// for Escape (same approach as Ryoku's own context menus).
PanelWindow {
    id: root

    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "device-hub-panel"
    WlrLayershell.keyboardFocus: root.visible ? WlrKeyboardFocus.Exclusive : WlrKeyboardFocus.None
    exclusionMode: ExclusionMode.Ignore
    color: "transparent"

    anchors { top: true; bottom: true; left: true; right: true }

    visible: false

    // ---- theme (standalone palette) --------------------------------------
    readonly property color bgColor: "#1a1b26"
    readonly property color cardColor: "#232433"
    readonly property color borderColor: "#2f334d"
    readonly property color textColor: "#c0caf5"
    readonly property color dimColor: "#7f8aa8"
    readonly property color accentColor: "#7aa2f7"
    readonly property color accentHover: "#9cb8ff"
    readonly property color hoverColor: "#2f334d"

    property var state: ({ devices: [] })
    readonly property var devices: state.devices || []
    readonly property var rgbMap: state.rgb || ({})
    readonly property var profiles: state.profiles || []
    readonly property string activeProfile: state.activeProfile || ""

    // ---- IPC -------------------------------------------------------------
    IpcHandler {
        target: "devicehub"
        function toggle(): void { root.visible = !root.visible }
        function open(): void { root.reload(); root.visible = true }
        function close(): void { root.visible = false }
    }

    // ---- state file ------------------------------------------------------
    // The daemon rewrites state.json in place, so atomicWrites stays off and
    // fileChanged only schedules a re-read; the parse happens in onLoaded,
    // once the new contents are actually available.
    FileView {
        id: stateFile
        path: (Quickshell.env("XDG_RUNTIME_DIR") || "/run/user/1000") + "/device-hub/state.json"
        watchChanges: true
        atomicWrites: false
        printErrors: false
        onFileChanged: stateFile.reload()
        onLoaded: root.reload()
    }

    function reload() {
        try {
            const text = stateFile.text()
            const parsed = text ? JSON.parse(text) : null
            if (parsed && parsed.devices) root.state = parsed
        } catch (e) {
            // keep the last good state while the file is mid-write
        }
    }

    // ---- control socket (panel -> daemon) --------------------------------
    Socket {
        id: control
        path: (Quickshell.env("XDG_RUNTIME_DIR") || "/run/user/1000") + "/device-hub/control.sock"
        connected: false
    }

    Timer {
        interval: 2000
        running: true
        repeat: true
        onTriggered: if (!control.connected) control.connected = true
    }

    Component.onCompleted: control.connected = true

    function send(cmd) {
        if (!control.connected) control.connected = true
        control.write(JSON.stringify(cmd) + "\n")
        control.flush()
    }

    onVisibleChanged: {
        if (!visible) return
        reload()
        Qt.callLater(() => keys.forceActiveFocus())
    }

    // ---- dismiss ---------------------------------------------------------
    // Fullscreen catcher below the card: any press outside closes the panel.
    MouseArea {
        anchors.fill: parent
        acceptedButtons: Qt.AllButtons
        z: 0
        onPressed: root.visible = false
    }

    // Keyboard focus holder: Escape closes, like any other popup.
    Item {
        id: keys
        anchors.fill: parent
        focus: true
        z: 1
        Keys.onPressed: event => {
            if (event.key === Qt.Key_Escape) {
                root.visible = false
                event.accepted = true
            }
        }
    }

    // ---- card ------------------------------------------------------------
    Rectangle {
        id: card

        width: 400
        height: content.implicitHeight + 28

        anchors.top: parent.top
        anchors.right: parent.right
        anchors.topMargin: 48
        anchors.rightMargin: 12

        z: 2

        radius: 16
        color: root.bgColor
        border.width: 1
        border.color: root.borderColor

        // Swallow presses on the card so they never reach the outside catcher.
        MouseArea {
            anchors.fill: parent
            acceptedButtons: Qt.AllButtons
        }

        Column {
            id: content

            anchors.top: parent.top
            anchors.left: parent.left
            anchors.right: parent.right
            anchors.margins: 14
            spacing: 12

            // Header
            Item {
                width: parent.width
                height: 28

                Text {
                    anchors.left: parent.left
                    anchors.verticalCenter: parent.verticalCenter
                    text: "Periféricos"
                    color: root.textColor
                    font.pixelSize: 17
                    font.bold: true
                }

                Text {
                    anchors.right: parent.right
                    anchors.verticalCenter: parent.verticalCenter
                    text: root.devices.length + " conectados"
                    color: root.dimColor
                    font.pixelSize: 11
                }
            }

            // Device cards, scrollable when the screen is short.
            Flickable {
                id: scroll
                width: parent.width
                height: Math.min(list.implicitHeight, root.height - 240)
                contentHeight: list.implicitHeight
                clip: true
                boundsBehavior: Flickable.StopAtBounds

                Column {
                    id: list
                    width: scroll.width
                    spacing: 8

                    Repeater {
                        model: root.devices

                        delegate: Rectangle {
                            id: cardItem
                            required property var modelData

                            readonly property string devId: modelData.id
                            readonly property var ledCfg: modelData.led || ({})
                            readonly property var controllers: ledCfg.controllers || []
                            readonly property bool hasLED: controllers.length > 0
                            readonly property bool ledOn: !!ledCfg.enabled
                            readonly property string activeController: ledCfg.controller || ""
                            readonly property bool hasBattery: !!modelData.hasBattery
                            readonly property bool online: modelData.connected !== false
                            readonly property int percent: Math.max(0, Math.min(100, modelData.percent || 0))
                            readonly property color levelColor: percent <= 20 ? "#f7768e"
                                                                        : percent <= 50 ? "#e0af68"
                                                                        : "#9ece6a"

                            width: list.width
                            height: 92
                            radius: 12
                            color: root.cardColor
                            border.width: 1
                            border.color: root.borderColor
                            opacity: online ? 1 : 0.6

                            Image {
                                id: art
                                anchors.left: parent.left
                                anchors.leftMargin: 10
                                anchors.verticalCenter: parent.verticalCenter
                                width: 52
                                height: 52
                                sourceSize.width: 104
                                sourceSize.height: 104
                                fillMode: Image.PreserveAspectFit
                                asynchronous: true
                                source: Qt.resolvedUrl("assets/" + (modelData.image || "unknown.svg"))
                            }

                            Column {
                                anchors.left: art.right
                                anchors.leftMargin: 10
                                anchors.right: rightColumn.left
                                anchors.rightMargin: 8
                                anchors.top: parent.top
                                anchors.topMargin: 12
                                spacing: 3

                                Text {
                                    width: parent.width
                                    text: modelData.name
                                    color: root.textColor
                                    font.pixelSize: 13
                                    font.bold: true
                                    elide: Text.ElideRight
                                }

                                Text {
                                    width: parent.width
                                    text: modelData.kind + (modelData.status && modelData.status !== "unknown"
                                                            ? " · " + modelData.status : "")
                                    color: root.dimColor
                                    font.pixelSize: 10
                                    elide: Text.ElideRight
                                }

                                // Controller selector: one chip per backend.
                                Flow {
                                    width: parent.width
                                    spacing: 4
                                    visible: cardItem.hasLED

                                    Repeater {
                                        model: cardItem.controllers

                                        delegate: Rectangle {
                                            id: chip
                                            required property string modelData
                                            readonly property bool active: cardItem.activeController === modelData
                                            readonly property bool hovered: chipArea.containsMouse && chipArea.enabled

                                            width: chipLabel.implicitWidth + 14
                                            height: 18
                                            radius: 9
                                            color: active ? root.accentColor
                                                  : hovered ? root.hoverColor
                                                  : "transparent"
                                            border.width: 1
                                            border.color: active ? root.accentColor
                                                       : hovered ? root.dimColor
                                                       : root.borderColor

                                            Text {
                                                id: chipLabel
                                                anchors.centerIn: parent
                                                text: modelData
                                                color: active ? "#1a1b26"
                                                     : hovered ? root.textColor
                                                     : root.dimColor
                                                font.pixelSize: 9
                                                font.bold: active
                                            }

                                            MouseArea {
                                                id: chipArea
                                                anchors.fill: parent
                                                hoverEnabled: true
                                                enabled: cardItem.online
                                                onClicked: root.send({
                                                    action: "controller",
                                                    device: cardItem.devId,
                                                    controller: modelData
                                                })
                                            }
                                        }
                                    }
                                }
                            }

                            // Battery level + LED switch.
                            Column {
                                id: rightColumn
                                anchors.right: parent.right
                                anchors.rightMargin: 12
                                anchors.top: parent.top
                                anchors.topMargin: 12
                                spacing: 6

                                Text {
                                    visible: cardItem.online && cardItem.hasBattery
                                    anchors.right: parent.right
                                    text: cardItem.percent + "%"
                                    color: cardItem.levelColor
                                    font.pixelSize: 16
                                    font.bold: true
                                }

                                Text {
                                    visible: !cardItem.online
                                    anchors.right: parent.right
                                    text: "desconectado"
                                    color: "#f7768e"
                                    font.pixelSize: 10
                                    font.bold: true
                                }

                                Rectangle {
                                    id: ledPill
                                    readonly property bool hovered: ledArea.containsMouse
                                    visible: cardItem.hasLED && cardItem.online
                                    anchors.right: parent.right
                                    width: 52
                                    height: 20
                                    radius: 10
                                    color: cardItem.ledOn
                                           ? (hovered ? root.accentHover : root.accentColor)
                                           : (hovered ? root.hoverColor : "transparent")
                                    border.width: 1
                                    border.color: cardItem.ledOn
                                                  ? (hovered ? root.accentHover : root.accentColor)
                                                  : (hovered ? root.dimColor : root.borderColor)

                                    Row {
                                        anchors.centerIn: parent
                                        spacing: 5

                                        Rectangle {
                                            width: 8
                                            height: 8
                                            radius: 4
                                            color: cardItem.ledOn
                                                   ? (root.rgbMap[cardItem.devId] || "#1a1b26")
                                                   : "transparent"
                                            border.width: 1
                                            border.color: cardItem.ledOn ? "#00000055" : root.dimColor
                                        }

                                        Text {
                                            text: "LED"
                                            color: cardItem.ledOn ? "#1a1b26" : root.dimColor
                                            font.pixelSize: 9
                                            font.bold: cardItem.ledOn
                                        }
                                    }

                                    MouseArea {
                                        id: ledArea
                                        anchors.fill: parent
                                        hoverEnabled: true
                                        onClicked: root.send({
                                            action: cardItem.ledOn ? "off" : "on",
                                            device: cardItem.devId
                                        })
                                    }
                                }
                            }

                            // Battery bar
                            Rectangle {
                                visible: cardItem.online && cardItem.hasBattery
                                anchors.left: art.right
                                anchors.leftMargin: 10
                                anchors.right: parent.right
                                anchors.rightMargin: 12
                                anchors.bottom: parent.bottom
                                anchors.bottomMargin: 10
                                height: 4
                                radius: 2
                                color: "#2f334d"

                                Rectangle {
                                    width: parent.width * (cardItem.percent / 100)
                                    height: parent.height
                                    radius: 2
                                    color: cardItem.levelColor
                                }
                            }
                        }
                    }
                }
            }

            // Footer: OpenRGB profile + open the official GUI.
            Column {
                width: parent.width
                spacing: 8

                Item {
                    width: parent.width
                    height: 18

                    Text {
                        anchors.left: parent.left
                        anchors.verticalCenter: parent.verticalCenter
                        text: "Perfil OpenRGB"
                        color: root.textColor
                        font.pixelSize: 12
                        font.bold: true
                    }

                    Text {
                        anchors.right: parent.right
                        anchors.verticalCenter: parent.verticalCenter
                        visible: root.profiles.length === 0
                        text: "sin profiles"
                        color: root.dimColor
                        font.pixelSize: 10
                    }
                }

                // One chip per profile on disk.
                Flow {
                    width: parent.width
                    spacing: 6
                    visible: root.profiles.length > 0

                    Repeater {
                        model: root.profiles

                        delegate: Rectangle {
                            required property string modelData
                            readonly property bool active: root.activeProfile === modelData
                            readonly property bool hovered: profileArea.containsMouse

                            width: profileLabel.implicitWidth + 18
                            height: 22
                            radius: 11
                            color: active ? (hovered ? root.accentHover : root.accentColor)
                                          : (hovered ? root.hoverColor : root.cardColor)
                            border.width: 1
                            border.color: active ? root.accentColor
                                       : hovered ? root.dimColor
                                       : root.borderColor

                            Text {
                                id: profileLabel
                                anchors.centerIn: parent
                                text: modelData
                                color: active ? "#1a1b26" : root.textColor
                                font.pixelSize: 10
                                font.bold: active
                            }

                            MouseArea {
                                id: profileArea
                                anchors.fill: parent
                                hoverEnabled: true
                                onClicked: root.send({ action: "profile", profile: modelData })
                            }
                        }
                    }
                }

                Rectangle {
                    width: parent.width
                    height: 34
                    radius: 8
                    color: openArea.containsMouse ? root.accentHover : root.accentColor

                    Text {
                        anchors.centerIn: parent
                        text: "Abrir OpenRGB"
                        color: "#1a1b26"
                        font.pixelSize: 12
                        font.bold: true
                    }

                    MouseArea {
                        id: openArea
                        anchors.fill: parent
                        hoverEnabled: true
                        onClicked: root.send({ action: "openrgb" })
                    }
                }
            }
        }
    }
}
