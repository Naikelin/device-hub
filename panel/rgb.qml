import QtQuick
import Quickshell
import Quickshell.Io

// Device Hub RGB window: the only place that changes LED colours.
//
// It is a regular floating window (not a layer-shell panel) so it can stay
// open next to the hub. The tray ("LED colors…") opens it through its own IPC
// target:
//
//   qs ipc -p <this file> call devicehub-rgb open
//
// State comes from the same state.json the hub reads; commands go back over
// the same control socket.
FloatingWindow {
    id: root

    title: "Device Hub · RGB"
    implicitWidth: 420
    implicitHeight: content.implicitHeight + 32
    visible: true
    color: root.bgColor

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
    readonly property var ledDevices: devices.filter(d => ((d.led || {}).controllers || []).length > 0)
    readonly property var targets: [{ id: "all", name: "All" }].concat(
        ledDevices.map(d => ({ id: d.id, name: d.name })))
    readonly property var palette: [
        "#ff4d4d", "#ff9f43", "#ffe066", "#51cf66",
        "#22d3ee", "#4dabf7", "#a78bfa", "#f8f9fa"
    ]

    property string target: "all"

    // ---- IPC -------------------------------------------------------------
    IpcHandler {
        target: "devicehub-rgb"
        function toggle(): void { root.visible = !root.visible }
        function open(): void { root.reload(); root.visible = true }
        function close(): void { root.visible = false }
    }

    Shortcut {
        sequence: "Escape"
        onActivated: root.visible = false
    }

    // ---- state file ------------------------------------------------------
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

    // ---- control socket (window -> daemon) -------------------------------
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

    Component.onCompleted: {
        control.connected = true
        reload()
    }

    function send(cmd) {
        if (!control.connected) control.connected = true
        control.write(JSON.stringify(cmd) + "\n")
        control.flush()
    }

    function applyColor(hex) {
        const clean = (hex || "").trim()
        if (!/^#?[0-9a-fA-F]{6}$/.test(clean)) return
        const value = clean.startsWith("#") ? clean : "#" + clean
        const cmd = { action: root.target === "all" ? "sync" : "set", color: value }
        if (cmd.action === "set") cmd.device = root.target
        root.send(cmd)
    }

    Column {
        id: content
        anchors.fill: parent
        anchors.margins: 16
        spacing: 12

        // Title row
        Item {
            width: parent.width
            height: 26

            Text {
                anchors.left: parent.left
                anchors.verticalCenter: parent.verticalCenter
                text: "RGB control"
                color: root.textColor
                font.pixelSize: 17
                font.bold: true
            }

            Text {
                anchors.right: parent.right
                anchors.verticalCenter: parent.verticalCenter
                text: "×"
                color: closeArea.containsMouse ? root.textColor : root.dimColor
                font.pixelSize: 20

                MouseArea {
                    id: closeArea
                    anchors.fill: parent
                    anchors.margins: -6
                    hoverEnabled: true
                    onClicked: root.visible = false
                }
            }
        }

        // Target chips
        Text {
            text: root.target === "all" ? "Apply to every device that is on" : "Apply to one device"
            color: root.dimColor
            font.pixelSize: 10
        }

        Flow {
            width: parent.width
            spacing: 6

            Repeater {
                model: root.targets

                delegate: Rectangle {
                    required property var modelData
                    readonly property bool active: root.target === modelData.id
                    readonly property bool hovered: targetArea.containsMouse

                    width: targetLabel.implicitWidth + 18
                    height: 24
                    radius: 12
                    color: active ? (hovered ? root.accentHover : root.accentColor)
                                  : (hovered ? root.hoverColor : root.cardColor)
                    border.width: 1
                    border.color: active ? root.accentColor
                               : hovered ? root.dimColor
                               : root.borderColor

                    Text {
                        id: targetLabel
                        anchors.centerIn: parent
                        text: modelData.name
                        color: active ? "#1a1b26" : root.textColor
                        font.pixelSize: 10
                        font.bold: active
                    }

                    MouseArea {
                        id: targetArea
                        anchors.fill: parent
                        hoverEnabled: true
                        onClicked: root.target = modelData.id
                    }
                }
            }
        }

        // Palette
        Row {
            width: parent.width
            spacing: 6

            Repeater {
                model: root.palette

                delegate: Rectangle {
                    required property string modelData

                    width: (parent.width - 7 * 6) / 8
                    height: 30
                    radius: 8
                    color: modelData
                    border.width: swatchArea.containsMouse ? 2 : 1
                    border.color: swatchArea.containsMouse ? "#ffffff" : "#00000055"

                    MouseArea {
                        id: swatchArea
                        anchors.fill: parent
                        hoverEnabled: true
                        onClicked: root.applyColor(modelData)
                    }
                }
            }
        }

        // Arbitrary hex colour
        Row {
            width: parent.width
            spacing: 8

            Rectangle {
                width: parent.width - applyButton.width - parent.spacing
                height: 30
                radius: 8
                color: root.cardColor
                border.width: 1
                border.color: root.borderColor

                TextInput {
                    id: hexInput
                    anchors.fill: parent
                    anchors.leftMargin: 10
                    anchors.rightMargin: 10
                    verticalAlignment: TextInput.AlignVCenter
                    color: root.textColor
                    font.pixelSize: 12
                    selectionColor: root.accentColor
                    selectedTextColor: "#1a1b26"
                    onAccepted: root.applyColor(text)
                }

                Text {
                    anchors.left: parent.left
                    anchors.leftMargin: 10
                    anchors.verticalCenter: parent.verticalCenter
                    visible: hexInput.text.length === 0
                    text: "#7aa2f7"
                    color: root.dimColor
                    font.pixelSize: 12
                }
            }

            Rectangle {
                id: applyButton
                width: 74
                height: 30
                radius: 8
                color: applyArea.containsMouse ? root.accentHover : root.accentColor

                Text {
                    anchors.centerIn: parent
                    text: "Apply"
                    color: "#1a1b26"
                    font.pixelSize: 11
                    font.bold: true
                }

                MouseArea {
                    id: applyArea
                    anchors.fill: parent
                    hoverEnabled: true
                    onClicked: root.applyColor(hexInput.text)
                }
            }
        }

        // Per-device state
        Rectangle {
            width: parent.width
            height: 1
            color: root.borderColor
        }

        Column {
            width: parent.width
            spacing: 6

            Repeater {
                model: root.ledDevices

                delegate: Row {
                    required property var modelData
                    readonly property var ledCfg: modelData.led || ({})

                    width: parent.width
                    spacing: 8

                    Rectangle {
                        width: 10
                        height: 10
                        radius: 5
                        color: ledCfg.enabled ? (root.rgbMap[modelData.id] || "#3b3f5c") : "transparent"
                        border.width: 1
                        border.color: ledCfg.enabled ? "#00000055" : root.dimColor
                    }

                    Text {
                        width: parent.width - 150
                        text: modelData.name
                        color: ledCfg.enabled ? root.textColor : root.dimColor
                        font.pixelSize: 11
                        elide: Text.ElideRight
                    }

                    Rectangle {
                        visible: ledCfg.enabled && ledCfg.controller === "openrgb"
                        width: appLabel.implicitWidth + 14
                        height: 18
                        radius: 9
                        color: appArea.containsMouse ? root.hoverColor : root.cardColor
                        border.width: 1
                        border.color: appArea.containsMouse ? root.dimColor : root.borderColor

                        Text {
                            id: appLabel
                            anchors.centerIn: parent
                            text: "OpenRGB"
                            color: root.textColor
                            font.pixelSize: 9
                        }

                        MouseArea {
                            id: appArea
                            anchors.fill: parent
                            hoverEnabled: true
                            onClicked: root.send({ action: "openrgb" })
                        }
                    }

                    Text {
                        visible: !ledCfg.enabled || ledCfg.controller !== "openrgb"
                        text: ledCfg.enabled ? (ledCfg.controller || "") : "off"
                        color: root.dimColor
                        font.pixelSize: 10
                    }
                }
            }
        }
    }
}
