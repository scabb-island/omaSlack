pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls
import qs.Commons
import qs.Commons as Commons
import qs.Ui
import "lib/window.js" as W

// The window's sidebar: each workspace with its Unread, Channels and
// Direct messages sections (rows of W.sidebarRows), unread conversations
// in bold with their mention count. A ListView over a KeyedModel, so a
// workspace with a thousand conversations builds only the rows on screen
// and keeps its scroll position when one of them changes.
//
// The window owns the cursor (cursorIndex) and the open conversation
// (currentKey); this draws them and reports picks.
ListView {
  id: root

  property var rows: []
  property int cursorIndex: -1
  // "<team>/<conv>" of the open conversation.
  property string currentKey: ""
  // Whether the sidebar has the keyboard (the cursor shows).
  property bool focused: false
  property color foreground: Commons.Color.foreground
  property string fontFamily: Style.font.family

  signal picked(int index)
  signal pointed(int index)

  property var rowByKey: ({})
  readonly property color dim: Qt.darker(foreground, 1.4)

  model: KeyedModel { id: keyed }
  clip: true
  boundsBehavior: Flickable.StopAtBounds
  keyNavigationEnabled: false
  currentIndex: cursorIndex
  highlightFollowsCurrentItem: false
  ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }

  onRowsChanged: {
    var byKey = ({})
    for (var i = 0; i < rows.length; i++) byKey[rows[i].key] = rows[i]
    rowByKey = byKey
    keyed.sync(rows.map(function(r) { return r.key }))
  }
  onCursorIndexChanged: if (cursorIndex >= 0) positionViewAtIndex(cursorIndex, ListView.Contain)

  delegate: Item {
    id: item
    required property string key
    required property int index
    readonly property var row: root.rowByKey[key] || ({ type: "" })

    width: ListView.view.width
    height: row.type === "conv" ? convRow.implicitHeight : heading.implicitHeight

    // Workspace and section headings.
    Item {
      id: heading
      visible: item.row.type !== "conv"
      width: parent.width
      implicitHeight: item.row.type === "workspace"
        ? workspaceLine.implicitHeight + Style.spacing.lg * 2
        : sectionText.implicitHeight + Style.spacing.sm

      Row {
        id: workspaceLine
        visible: item.row.type === "workspace"
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.bottom: parent.bottom
        anchors.leftMargin: Style.spacing.sm
        spacing: Style.spacing.md

        Text {
          textFormat: Text.PlainText
          text: item.row.name || ""
          color: root.foreground
          font.family: root.fontFamily
          font.pixelSize: Style.font.subtitle
          font.bold: true
        }
        Text {
          visible: text !== ""
          textFormat: Text.PlainText
          text: item.row.state && item.row.state !== "connected" ? item.row.state.replace("_", " ") : ""
          color: item.row.state === "auth_expired" ? Commons.Color.urgent : root.dim
          font.family: root.fontFamily
          font.pixelSize: Style.font.caption
        }
      }

      PanelSectionHeader {
        id: sectionText
        visible: item.row.type === "section"
        anchors.left: parent.left
        anchors.bottom: parent.bottom
        anchors.leftMargin: Style.spacing.sm
        text: item.row.title || ""
        foreground: root.foreground
        fontFamily: root.fontFamily
      }
    }

    // A conversation.
    CursorSurface {
      id: convRow
      objectName: "convRow"
      visible: item.row.type === "conv"
      width: parent.width
      implicitHeight: convName.implicitHeight + Style.spacing.xs * 2
      hasCursor: root.focused && root.cursorIndex === item.index
      current: root.currentKey === item.key
      foreground: root.foreground

      MouseArea {
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onEntered: root.pointed(item.index)
        onClicked: root.picked(item.index)
      }

      Text {
        id: convName
        objectName: "convName"
        anchors.left: parent.left
        anchors.right: badge.left
        anchors.verticalCenter: parent.verticalCenter
        anchors.leftMargin: Style.spacing.rowPaddingX
        anchors.rightMargin: Style.spacing.sm
        textFormat: Text.PlainText
        text: item.row.name || ""
        color: item.row.unread ? root.foreground : root.dim
        font.family: root.fontFamily
        font.pixelSize: Style.font.body
        font.bold: !!item.row.unread
        elide: Text.ElideRight
      }

      Rectangle {
        id: badge
        visible: !!item.row.badge
        anchors.right: parent.right
        anchors.rightMargin: Style.spacing.md
        anchors.verticalCenter: parent.verticalCenter
        width: visible ? Math.max(height, badgeText.implicitWidth + Style.spacing.md * 2) : 0
        height: badgeText.implicitHeight + Style.spacing.xxs * 2
        radius: height / 2
        color: Commons.Color.accent

        Text {
          id: badgeText
          anchors.centerIn: parent
          textFormat: Text.PlainText
          text: item.row.badge || ""
          color: Commons.Color.background
          font.family: root.fontFamily
          font.pixelSize: Style.font.caption
          font.bold: true
        }
      }
    }
  }
}
