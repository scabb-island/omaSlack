import QtQuick
import QtTest
import "../lib/actions.js" as A

// The window's message actions (lib/actions.js): who may edit and delete,
// the reaction toggle, the emoji picker's list and filter, opening files
// and the :attach command.
TestCase {
  name: "Actions"

  readonly property var mine: ({ ts: "1.000001", author: "U1", text: "hi", reactions: [{ emoji: "tada", users: ["U1", "U2"] }, { emoji: "eyes", users: ["U2"] }] })
  readonly property var theirs: ({ ts: "2.000001", author: "U2", text: "yo" })

  // ---- edit and delete --------------------------------------------------------

  function test_only_your_own_messages_can_be_edited_or_deleted() {
    compare(A.ownMessage(mine, "U1", "edit"), "")
    compare(A.ownMessage(theirs, "U1", "delete"), "You can only delete your own messages.")
    compare(A.ownMessage(null, "U1", "edit"), "Select a message to edit first.")
  }

  function test_an_unknown_self_lets_the_action_through_as_the_tui_does() {
    // The daemon may not know who the user is yet; Slack then refuses.
    compare(A.ownMessage(theirs, "", "edit"), "")
  }

  function test_only_y_confirms_a_delete() {
    verify(A.confirmsDelete("y"))
    verify(A.confirmsDelete("Y"))
    verify(!A.confirmsDelete("n"))
    verify(!A.confirmsDelete(""))
  }

  // ---- reactions --------------------------------------------------------------

  function test_a_reaction_toggles_on_whether_you_already_reacted() {
    compare(A.reactionAdds(mine, "tada", "U1"), false) // yours: remove it
    compare(A.reactionAdds(mine, "eyes", "U1"), true)  // others' only: add yours
    compare(A.reactionAdds(mine, "fire", "U1"), true)  // new
    compare(A.reactionAdds(theirs, "fire", "U1"), true) // no reactions at all
  }

  function test_with_self_unknown_a_reaction_always_adds() {
    compare(A.reactionAdds(mine, "tada", ""), true)
  }

  function test_standard_emoji_have_their_character() {
    compare(A.emojiGlyph("+1"), "👍")
    compare(A.emojiGlyph("tada"), "🎉")
    compare(A.emojiGlyph("partyparrot"), "") // custom: no character
  }

  function test_emoji_names_are_the_standard_ones_then_the_custom_ones_sorted() {
    var names = A.emojiNames({ zebra: "https://x/z.png", ace: "alias:+1", tada: "https://x/t.png" })
    compare(names[0], "+1")
    compare(names.slice(-2), ["ace", "zebra"])
    compare(names.filter(function(n) { return n === "tada" }).length, 1) // a custom one shadowing a standard one is listed once
    verify(names.length > 100)
  }

  function test_the_filter_ranks_exact_then_prefix_then_word_start_then_substring() {
    var names = ["heart_eyes", "broken_heart", "heart", "sweetheart", "fire"]
    compare(A.filterEmoji(names, [], "heart", 10), ["heart", "heart_eyes", "broken_heart", "sweetheart"])
  }

  function test_the_filter_ignores_colons_and_case_and_keeps_the_order_when_empty() {
    compare(A.filterEmoji(["b", "a"], [], ":A:", 10), ["a"])
    compare(A.filterEmoji(["b", "a"], [], "", 10), ["b", "a"])
  }

  function test_recent_emoji_come_first_among_equal_matches_and_the_limit_holds() {
    var names = ["fire", "fireworks", "firecracker", "tada"]
    compare(A.filterEmoji(names, ["firecracker"], "fire", 10), ["fire", "firecracker", "fireworks"])
    compare(A.filterEmoji(names, ["tada"], "", 2), ["tada", "fire"])
    compare(A.filterEmoji(names, ["gone"], "", 10), ["fire", "fireworks", "firecracker", "tada"]) // unknown recent ones are dropped
  }

  function test_remembering_an_emoji_puts_it_first_once_and_keeps_twelve() {
    compare(A.rememberEmoji(["a", "b"], "b"), ["b", "a"])
    var many = []
    for (var i = 0; i < 12; i++) many.push("e" + i)
    var r = A.rememberEmoji(many, "new")
    compare(r.length, 12)
    compare(r[0], "new")
    compare(r[11], "e10")
  }

  function test_picking_takes_the_selected_match_or_the_typed_name() {
    compare(A.pickEmoji(["fire", "fireworks"], 1, "fire"), "fireworks")
    // Nothing listed matches: react with the typed name (Slack checks it).
    compare(A.pickEmoji([], 0, ":partyparrot:"), "partyparrot")
    compare(A.pickEmoji([], 0, "not a name"), "")
    compare(A.pickEmoji([], 0, ""), "")
  }

  // ---- files ------------------------------------------------------------------

  function test_o_opens_the_only_file_or_offers_a_choice() {
    compare(A.fileChoice(undefined).kind, "none")
    compare(A.fileChoice([]).kind, "none")
    var one = A.fileChoice([{ id: "F1" }])
    compare(one.kind, "open")
    compare(one.file.id, "F1")
    var two = A.fileChoice([{ id: "F1" }, { id: "F2" }])
    compare(two.kind, "choose")
    compare(two.files.length, 2)
  }

  function test_images_are_files_with_an_image_mime_type() {
    verify(A.isImage({ mimetype: "image/png" }))
    verify(!A.isImage({ mimetype: "application/pdf" }))
    verify(!A.isImage({}))
  }

  function test_files_need_the_workspace_capability() {
    verify(A.canFiles({ capabilities: ["files", "search"] }))
    verify(!A.canFiles({ capabilities: ["search"] }))
    verify(!A.canFiles(null))
  }

  function test_file_name_falls_back_to_the_title_then_the_id() {
    compare(A.fileName({ id: "F1", name: "a.pdf", title: "A" }), "a.pdf")
    compare(A.fileName({ id: "F1", title: "A" }), "A")
    compare(A.fileName({ id: "F1" }), "F1")
  }

  // ---- :attach ------------------------------------------------------------------

  function test_attach_takes_a_path_and_a_caption() {
    compare(A.parseAttach("hello", "/home/ana"), null)
    compare(A.parseAttach(":attachment", "/home/ana"), null)
    compare(A.parseAttach(":attach /tmp/a.png", "/home/ana"), { path: "/tmp/a.png", caption: "", error: "" })
    compare(A.parseAttach("  :attach /tmp/a.png  look at this ", "/home/ana"), { path: "/tmp/a.png", caption: "look at this", error: "" })
  }

  function test_attach_takes_quoted_paths_and_the_home_directory() {
    compare(A.parseAttach(":attach \"/tmp/my file.png\" caption", "/home/ana"), { path: "/tmp/my file.png", caption: "caption", error: "" })
    compare(A.parseAttach(":attach '~/a b.txt'", "/home/ana"), { path: "/home/ana/a b.txt", caption: "", error: "" })
    compare(A.parseAttach(":attach ~", "/home/ana"), { path: "/home/ana", caption: "", error: "" })
  }

  function test_attach_explains_what_it_cannot_take() {
    compare(A.parseAttach(":attach", "/home/ana").error, "Usage: :attach <path> [caption]")
    compare(A.parseAttach(":attach   ", "/home/ana").error, "Usage: :attach <path> [caption]")
    // The shell has no working directory of the user's: paths are absolute.
    compare(A.parseAttach(":attach notes.txt", "/home/ana").error, "Use an absolute path or ~/: notes.txt")
  }
}
