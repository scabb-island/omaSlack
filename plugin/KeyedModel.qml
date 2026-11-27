import QtQuick
import "lib/window.js" as W

// A ListModel of keys only ({ key }), kept equal to a list of keys with the
// fewest inserts, removes and moves (lib/window.js syncOps). The window's
// lists use it instead of a plain JS array model, which a ListView rebuilds
// from scratch, losing the scroll position, on every change. Delegates look
// their data up by key.
ListModel {
  id: root

  property var keys: []

  // sync makes the model hold next, in order.
  function sync(next) {
    var ops = W.syncOps(root.keys, next)
    for (var i = 0; i < ops.length; i++) {
      var op = ops[i]
      if (op.op === "remove") root.remove(op.index, 1)
      else if (op.op === "move") root.move(op.from, op.to, 1)
      else root.insert(op.index, { key: op.key })
    }
    root.keys = next
  }
}
