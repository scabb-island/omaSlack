import QtQuick
import qs.Commons
import qs.Ui
import "lib/actions.js" as A
import "lib/filecache.js" as Files

// An image file's thumbnail in a message. It asks the daemon for the
// file (files.fetch, once per file through lib/filecache.js) when it is
// built, which in the virtualized MessageList is when its message comes
// near the screen, and decodes it asynchronously at the drawn size
// (SizedImage). It stays out of the layout until the picture is there,
// and when the workspace has no files capability.
//
// The image keeps its own size, never this item's laid-out one: whether
// the thumb shows depends on the decode, so a decode that depended on the
// thumb showing was a cycle (visible -> height -> decode -> broken ->
// visible) that Qt reported as a binding loop.
Item {
  id: root

  property var service: null
  property string team: ""
  // A wire.File with an image/* mimetype.
  property var file: null
  // The widest the thumbnail may be (the message body's width).
  property real maxWidth: implicitWidth

  signal clicked()

  readonly property string want: file && file.id ? team + "/" + file.id : ""
  readonly property bool allowed: !!service && A.can(workspaceOf(service.workspaces, team), "files")
  property string path: ""
  // The picture could not be decoded (set by the image).
  property bool broken: false
  readonly property url url: path !== "" ? A.fileUrl(path) : ""

  objectName: "thumb:" + (file && file.id ? file.id : "")
  visible: path !== "" && !broken
  implicitWidth: Style.space(320)
  implicitHeight: visible ? image.height : 0

  function workspaceOf(list, t) {
    for (var i = 0; i < (list || []).length; i++) if (list[i].team === t) return list[i]
    return null
  }

  function load() {
    path = ""
    broken = false
    if (!allowed || want === "") return
    var asked = want
    Files.path(service, team, file.id, function(p) { if (root && root.want === asked) root.path = p })
  }

  // Delegates are reused for other messages: the file can change.
  onWantChanged: load()
  onAllowedChanged: load()
  Component.onCompleted: load()

  SizedImage {
    id: image
    width: Math.min(root.implicitWidth, root.maxWidth)
    height: Style.space(200)
    url: root.url
    horizontalAlignment: Image.AlignLeft
    onStatusChanged: if (status === Image.Error) root.broken = true
  }

  MouseArea {
    anchors.fill: parent
    cursorShape: Qt.PointingHandCursor
    onClicked: root.clicked()
  }
}
