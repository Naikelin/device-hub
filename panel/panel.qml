import QtQuick
import Quickshell
import Quickshell.Io
import Quickshell.Wayland

PanelWindow {
    id: root

    WlrLayershell.layer: WlrLayer.Top
    WlrLayershell.namespace: "device-hub-panel"
    WlrLayershell.keyboardFocus: WlrKeyboardFocus.OnDemand
    exclusionMode: ExclusionMode.Ignore
    color: "transparent"

    // Anclado arriba a la derecha (junto al tray).
    anchors { top: true; right: true }
    margins.top: 52
    margins.right: 16

    visible: false
    implicitWidth: 340
    implicitHeight: 240

    FileView {
        id: stateFile
        path: "/run/user/1000/device-hub/state.json"
    }

    property var state: {
        var t = stateFile.text()
        return t ? JSON.parse(t) : ({ devices: [], toggle: 0 })
    }
    property int toggle: state ? (state.toggle || 0) : 0

    // Cada click en el tray incrementa toggle -> alterna visibilidad.
    onToggleChanged: {
        if (toggle > 0) root.visible = !root.visible
    }

    Rectangle {
        anchors.fill: parent
        color: "#1b1b27"
        radius: 16
        border.color: "#34344a"
        border.width: 1

        Column {
            anchors.fill: parent
            anchors.margins: 18
            spacing: 14

            Text {
                text: "Periféricos"
                color: "white"
                font.pixelSize: 18
                font.bold: true
            }

            Repeater {
                model: root.state.devices || []
                delegate: Row {
                    width: parent.width
                    spacing: 12
                    Text {
                        text: modelData.name
                        color: "#c8c8d4"
                        width: parent.width - 64
                        elide: Text.ElideRight
                        font.pixelSize: 14
                    }
                    Text {
                        text: modelData.percent + "%"
                        color: "white"
                        font.bold: true
                        font.pixelSize: 14
                    }
                }
            }
        }
    }
}
