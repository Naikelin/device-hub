import QtQuick
import Quickshell
import Quickshell.Io
import Quickshell.Wayland

// Device Hub panel: popup with one card per peripheral.
//
// State comes from the daemon through a JSON file (Quickshell 0.3.x has no
// generic D-Bus client), while show/hide is driven by IPC so the tray click
// feels instant:
//
//   qs ipc -p <this file> call devicehub toggle
//
// The window is a fullscreen transparent Overlay layer instead of a small
// anchored surface: the empty area is the outside-click catcher, so clicking
// anywhere off the card dismisses the panel, and the surface can take keyboard
// focus for Escape. Same approach as Ryoku's own context menus.
PanelWindow {
    id: root

    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "device-hub-panel"
    WlrLayershell.keyboardFocus: root.visible ? WlrKeyboardFocus.Exclusive : WlrKeyboardFocus.None
    exclusionMode: ExclusionMode.Ignore
    color: "transparent"

    anchors { top: true; bottom: true; left: true; right: true }

    visible: false

    // ---- theme (standalone palette; swaps to qs.Commons when embedded) ----
    readonly property color bgColor: "#1a1b26"
    readonly property color cardColor: "#232433"
    readonly property color borderColor: "#2f334d"
    readonly property color textColor: "#c0caf5"
    readonly property color dimColor: "#7f8aa8"
    readonly property color accentColor: "#7aa2f7"

    property var state: ({ devices: [] })
    readonly property var devices: state.devices || []

    // ---- IPC -------------------------------------------------------------
    IpcHandler {
        target: "devicehub"
        function toggle(): void { root.visible = !root.visible }
        function open(): void { root.reload(); root.visible = true }
        function close(): void { root.visible = false }
    }

    // ---- state file ------------------------------------------------------
    FileView {
        id: stateFile
        path: (Quickshell.env("XDG_RUNTIME_DIR") || "/run/user/1000") + "/device-hub/state.json"
        watchChanges: true
        printErrors: false
        onFileChanged: root.reload()
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
        }
    }
}
