import QtQuick
import Quickshell
import Quickshell.Io
import Quickshell.Wayland

// Device Hub panel: popup with one card per peripheral plus RGB control.
//
// Battery and RGB state come from the daemon through a JSON file (Quickshell
// 0.3.x has no generic D-Bus client). Show/hide is driven by IPC so the tray
// click feels instant:
//
//   qs ipc -p <this file> call devicehub toggle
//
// RGB commands go back to the daemon over a Unix socket, one JSON line per
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

    property var state: ({ devices: [] })
    readonly property var devices: state.devices || []
    readonly property var rgbMap: state.rgb || ({})
    readonly property var rgbDevices: devices.filter(d => d.rgb)
    readonly property var targetModel: [{ id: "all", name: "Todos" }].concat(
        rgbDevices.map(d => ({ id: d.id, name: d.name })))
    readonly property var palette: [
        "#ff4d4d", "#ff9f43", "#ffe066", "#51cf66",
        "#22d3ee", "#4dabf7", "#a78bfa", "#f8f9fa"
    ]

    property string rgbTarget: "all"

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

    function applyColor(hex) {
        const action = root.rgbTarget === "all" ? "sync" : "set"
        const cmd = { action: action, color: hex }
        if (action === "set") cmd.device = root.rgbTarget
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

        width: 360
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

            // Device cards
            Column {
                id: list
                width: parent.width
                spacing: 8

                Repeater {
                    model: root.devices

                    delegate: Rectangle {
                        required property var modelData
                        width: list.width
                        height: 76
                        radius: 12
                        color: root.cardColor
                        border.width: 1
                        border.color: root.borderColor

                        readonly property int percent: Math.max(0, Math.min(100, modelData.percent || 0))
                        readonly property color levelColor: percent <= 20 ? "#f7768e"
                                                                    : percent <= 50 ? "#e0af68"
                                                                    : "#9ece6a"

                        Image {
                            id: art
                            anchors.left: parent.left
                            anchors.leftMargin: 10
                            anchors.verticalCenter: parent.verticalCenter
                            width: 56
                            height: 56
                            sourceSize.width: 112
                            sourceSize.height: 112
                            fillMode: Image.PreserveAspectFit
                            asynchronous: true
                            source: Qt.resolvedUrl("assets/" + (modelData.image || "unknown.svg"))
                        }

                        Column {
                            anchors.left: art.right
                            anchors.leftMargin: 10
                            anchors.right: batteryText.left
                            anchors.rightMargin: 10
                            anchors.verticalCenter: parent.verticalCenter
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
                        }

                        Text {
                            id: batteryText
                            anchors.right: parent.right
                            anchors.rightMargin: 12
                            anchors.top: parent.top
                            anchors.topMargin: 12
                            text: parent.percent + "%"
                            color: parent.levelColor
                            font.pixelSize: 16
                            font.bold: true
                        }

                        // Current RGB color of an RGB-capable device.
                        Rectangle {
                            visible: modelData.rgb
                            width: 10
                            height: 10
                            radius: 5
                            color: root.rgbMap[modelData.id] || "#3b3f5c"
                            border.width: 1
                            border.color: "#00000055"
                            anchors.right: parent.right
                            anchors.rightMargin: 12
                            anchors.bottom: parent.bottom
                            anchors.bottomMargin: 20
                        }

                        // Battery bar
                        Rectangle {
                            anchors.left: art.right
                            anchors.leftMargin: 10
                            anchors.right: parent.right
                            anchors.rightMargin: 12
                            anchors.bottom: parent.bottom
                            anchors.bottomMargin: 12
                            height: 4
                            radius: 2
                            color: "#2f334d"

                            Rectangle {
                                width: parent.width * (parent.parent.percent / 100)
                                height: parent.height
                                radius: 2
                                color: parent.parent.levelColor
                            }
                        }
                    }
                }
            }

            // Divider
            Rectangle {
                width: parent.width
                height: 1
                color: root.borderColor
            }

            // RGB section
            Column {
                width: parent.width
                spacing: 8

                Item {
                    width: parent.width
                    height: 20

                    Text {
                        anchors.left: parent.left
                        anchors.verticalCenter: parent.verticalCenter
                        text: "RGB"
                        color: root.textColor
                        font.pixelSize: 13
                        font.bold: true
                    }

                    Text {
                        anchors.right: parent.right
                        anchors.verticalCenter: parent.verticalCenter
                        text: root.rgbTarget === "all" ? "sincronizar todos" : "solo un dispositivo"
                        color: root.dimColor
                        font.pixelSize: 10
                    }
                }

                // Target chips: "Todos" plus each RGB device.
                Flow {
                    width: parent.width
                    spacing: 6

                    Repeater {
                        model: root.targetModel

                        delegate: Rectangle {
                            required property var modelData
                            readonly property bool active: root.rgbTarget === modelData.id

                            width: label.implicitWidth + 18
                            height: 24
                            radius: 12
                            color: active ? root.accentColor : root.cardColor
                            border.width: 1
                            border.color: active ? root.accentColor : root.borderColor

                            Text {
                                id: label
                                anchors.centerIn: parent
                                text: modelData.name
                                color: active ? "#1a1b26" : root.textColor
                                font.pixelSize: 10
                                font.bold: active
                            }

                            MouseArea {
                                anchors.fill: parent
                                onClicked: root.rgbTarget = modelData.id
                            }
                        }
                    }
                }

                // Palette: click a swatch to apply it to the selected target.
                Row {
                    width: parent.width
                    spacing: 6

                    Repeater {
                        model: root.palette

                        delegate: Rectangle {
                            required property string modelData

                            width: (parent.width - 7 * 6) / 8
                            height: 28
                            radius: 8
                            color: modelData
                            border.width: 1
                            border.color: "#00000055"

                            MouseArea {
                                anchors.fill: parent
                                onClicked: root.applyColor(modelData)
                            }
                        }
                    }
                }
            }
        }
    }
}
