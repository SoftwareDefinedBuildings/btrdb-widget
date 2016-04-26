import QtQuick 2.0
import BTrDBTools 1.0

Rectangle {
    id: root

    width: 1024
    height: 768
    color: "grey"

    BTrDBPlotter {
      width: 500
      height: 100
      anchors.fill : parent
      anchors.margins : 20
    }
}
