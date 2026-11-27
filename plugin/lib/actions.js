.pragma library

// The window's message actions (task 4.5), pure so qmltestrunner covers
// them (tests/tst_actions.qml): who may edit and delete a message, the
// reaction toggle, the reaction picker's emoji list and filter, opening a
// message's files and the composer's :attach command. They follow the TUI
// (internal/tui/app: edit.go, reactpicker.go, files.go). MessageActions.qml
// turns them into Service calls.

// ---- edit and delete ------------------------------------------------------------

// ownMessage says why the user may not verb ("edit", "delete") message,
// "" when they may: it is theirs. While the daemon does not say who the
// user is (self ""), the action goes through and Slack refuses it if the
// message is not theirs, as in the TUI.
function ownMessage(message, self, verb) {
  if (!message || !message.ts) return "Select a message to " + verb + " first."
  if (self && message.author !== self) return "You can only " + verb + " your own messages."
  return ""
}

// confirmsDelete: the answer to "Delete this message? y/n"; anything but
// y keeps it.
function confirmsDelete(text) { return text === "y" || text === "Y" }

// ---- reactions ------------------------------------------------------------------

// reactionAdds: whether picking emoji on message adds a reaction (true)
// or removes the user's own (false). With self unknown it always adds.
function reactionAdds(message, emoji, self) {
  var list = (message && message.reactions) || []
  for (var i = 0; i < list.length; i++)
    if (self && list[i].emoji === emoji && (list[i].users || []).indexOf(self) >= 0) return false
  return true
}

// STANDARD: common standard emoji, by Slack name, in the picker's order.
// The daemon's full table (internal/mrkdwn, ~400 names) has no RPC; this
// subset was taken from it, so names and characters match what the daemon
// renders. Any other name still works: typed in the picker, it is sent as
// is and Slack checks it. Custom emoji come from emoji.list.
var STANDARD = [
  ["+1", "👍"],
  ["-1", "👎"],
  ["thumbsup", "👍"],
  ["thumbsdown", "👎"],
  ["ok_hand", "👌"],
  ["clap", "👏"],
  ["raised_hands", "🙌"],
  ["pray", "🙏"],
  ["muscle", "💪"],
  ["wave", "👋"],
  ["point_up", "☝️"],
  ["point_right", "👉"],
  ["eyes", "👀"],
  ["heart", "❤️"],
  ["orange_heart", "🧡"],
  ["yellow_heart", "💛"],
  ["green_heart", "💚"],
  ["blue_heart", "💙"],
  ["purple_heart", "💜"],
  ["broken_heart", "💔"],
  ["sparkling_heart", "💖"],
  ["100", "💯"],
  ["fire", "🔥"],
  ["sparkles", "✨"],
  ["star", "⭐"],
  ["tada", "🎉"],
  ["confetti_ball", "🎊"],
  ["balloon", "🎈"],
  ["gift", "🎁"],
  ["trophy", "🏆"],
  ["medal", "🏅"],
  ["white_check_mark", "✅"],
  ["heavy_check_mark", "✔️"],
  ["x", "❌"],
  ["warning", "⚠️"],
  ["no_entry", "⛔"],
  ["question", "❓"],
  ["exclamation", "❗"],
  ["bangbang", "‼️"],
  ["heavy_plus_sign", "➕"],
  ["heavy_minus_sign", "➖"],
  ["smile", "😄"],
  ["smiley", "😃"],
  ["grinning", "😀"],
  ["grin", "😁"],
  ["laughing", "😆"],
  ["joy", "😂"],
  ["rolling_on_the_floor_laughing", "🤣"],
  ["sweat_smile", "😅"],
  ["slightly_smiling_face", "🙂"],
  ["upside_down_face", "🙃"],
  ["wink", "😉"],
  ["blush", "😊"],
  ["innocent", "😇"],
  ["heart_eyes", "😍"],
  ["star-struck", "🤩"],
  ["kissing_heart", "😘"],
  ["yum", "😋"],
  ["stuck_out_tongue", "😛"],
  ["zany_face", "🤪"],
  ["hugging_face", "🤗"],
  ["thinking_face", "🤔"],
  ["shushing_face", "🤫"],
  ["raised_eyebrow", "🤨"],
  ["neutral_face", "😐"],
  ["expressionless", "😑"],
  ["no_mouth", "😶"],
  ["smirk", "😏"],
  ["unamused", "😒"],
  ["face_with_rolling_eyes", "🙄"],
  ["grimacing", "😬"],
  ["relieved", "😌"],
  ["pensive", "😔"],
  ["sleepy", "😪"],
  ["sleeping", "😴"],
  ["mask", "😷"],
  ["nauseated_face", "🤢"],
  ["exploding_head", "🤯"],
  ["cowboy_hat_face", "🤠"],
  ["partying_face", "🥳"],
  ["sunglasses", "😎"],
  ["nerd_face", "🤓"],
  ["confused", "😕"],
  ["worried", "😟"],
  ["open_mouth", "😮"],
  ["astonished", "😲"],
  ["flushed", "😳"],
  ["pleading_face", "🥺"],
  ["fearful", "😨"],
  ["cold_sweat", "😰"],
  ["cry", "😢"],
  ["sob", "😭"],
  ["scream", "😱"],
  ["disappointed", "😞"],
  ["sweat", "😓"],
  ["weary", "😩"],
  ["triumph", "😤"],
  ["rage", "😡"],
  ["skull", "💀"],
  ["poop", "💩"],
  ["clown_face", "🤡"],
  ["ghost", "👻"],
  ["robot_face", "🤖"],
  ["see_no_evil", "🙈"],
  ["hear_no_evil", "🙉"],
  ["speak_no_evil", "🙊"],
  ["melting_face", "🫠"],
  ["saluting_face", "🫡"],
  ["rocket", "🚀"],
  ["bulb", "💡"],
  ["memo", "📝"],
  ["pushpin", "📌"],
  ["link", "🔗"],
  ["lock", "🔒"],
  ["key", "🔑"],
  ["bell", "🔔"],
  ["zap", "⚡"],
  ["boom", "💥"],
  ["coffee", "☕"],
  ["beer", "🍺"],
  ["beers", "🍻"],
  ["pizza", "🍕"],
  ["cake", "🍰"],
  ["hourglass", "⌛"],
  ["alarm_clock", "⏰"],
  ["calendar", "📆"],
  ["chart_with_upwards_trend", "📈"],
  ["bar_chart", "📊"],
  ["construction", "🚧"],
  ["rotating_light", "🚨"],
  ["checkered_flag", "🏁"],
  ["crown", "👑"],
  ["gem", "💎"],
  ["moneybag", "💰"],
]

var GLYPHS = (function() {
  var out = ({})
  STANDARD.forEach(function(e) { out[e[0]] = e[1] })
  return out
})()

// emojiGlyph: a standard emoji's character; "" for a custom one (an image).
function emojiGlyph(name) { return GLYPHS.hasOwnProperty(name) ? GLYPHS[name] : "" }

// emojiNames: the picker's names: the standard ones, then the workspace's
// custom ones (emoji.list's { name: url | "alias:<name>" }) by name.
function emojiNames(custom) {
  var names = STANDARD.map(function(e) { return e[0] })
  var extra = Object.keys(custom || {}).filter(function(n) { return !GLYPHS.hasOwnProperty(n) }).sort()
  return names.concat(extra)
}

function bare(query) { return String(query || "").trim().replace(/^:+|:+$/g, "").toLowerCase() }

// matchScore: 3 exact, 2 prefix, 1 at the start of a word (after _ or -),
// 0 anywhere else; -1 when name does not contain query.
function matchScore(name, query) {
  var at = name.indexOf(query)
  if (at < 0) return -1
  if (name === query) return 3
  if (at === 0) return 2
  for (; at > 0; at = name.indexOf(query, at + 1))
    if (name[at - 1] === "_" || name[at - 1] === "-") return 1
  return 0
}

// filterEmoji: at most limit of names matching query (colons and case
// ignored), best match first; among equal matches the recent ones (most
// recent first), then the order of names. Recent names not in names are
// dropped.
function filterEmoji(names, recent, query, limit) {
  var q = bare(query)
  var scored = []
  names.forEach(function(n, i) {
    var s = q === "" ? 0 : matchScore(n, q)
    if (s < 0) return
    var r = (recent || []).indexOf(n)
    scored.push({ name: n, score: s, recent: r < 0 ? Infinity : r, order: i })
  })
  scored.sort(function(a, b) { return (b.score - a.score) || (a.recent - b.recent) || (a.order - b.order) })
  return scored.slice(0, limit).map(function(s) { return s.name })
}

// The picker remembers this many recent emoji.
var RECENT = 12

// rememberEmoji: recent with name first, once, at most RECENT long.
function rememberEmoji(recent, name) {
  return [name].concat((recent || []).filter(function(n) { return n !== name })).slice(0, RECENT)
}

// pickEmoji: the emoji to react with: the selected match, else the typed
// name when it looks like one (Slack checks that it exists); "" for none.
function pickEmoji(ranked, index, query) {
  if (index >= 0 && index < ranked.length) return ranked[index]
  var q = bare(query)
  return /^[a-z0-9_+'-]+$/.test(q) ? q : ""
}

// ---- files ----------------------------------------------------------------------

// fileChoice: what o does with a message's files: nothing, open the only
// one, or offer a choice.
function fileChoice(files) {
  var list = files || []
  if (list.length === 0) return { kind: "none" }
  if (list.length === 1) return { kind: "open", file: list[0] }
  return { kind: "choose", files: list }
}

function isImage(file) { return /^image\//.test((file && file.mimetype) || "") }

// canFiles: whether the workspace may fetch and upload files (its
// "files" capability).
function canFiles(workspace) { return !!workspace && (workspace.capabilities || []).indexOf("files") >= 0 }

function fileName(f) { return f.name || f.title || f.id || "the file" }

// ---- :attach ----------------------------------------------------------------------

var ATTACH_USAGE = "Usage: :attach <path> [caption]"

// parseAttach reads ":attach <path> [caption]" from a composer's text: a
// path with spaces goes in quotes and ~ is home. null when the text is
// not that command; { path, caption, error } otherwise, error "" when the
// path can be uploaded. Paths must be absolute: the shell has no working
// directory of the user's.
function parseAttach(text, home) {
  var m = String(text || "").trim().match(/^:attach(?:\s+([\s\S]*))?$/)
  if (!m) return null
  var arg = (m[1] || "").trim()
  if (arg === "") return { path: "", caption: "", error: ATTACH_USAGE }
  var path, caption
  var quoted = arg.match(/^(["'])(.*?)\1\s*([\s\S]*)$/)
  if (quoted) { path = quoted[2]; caption = quoted[3] }
  else { var sp = arg.search(/\s/); path = sp < 0 ? arg : arg.slice(0, sp); caption = sp < 0 ? "" : arg.slice(sp) }
  if (path === "~" || path.indexOf("~/") === 0) path = home + path.slice(1)
  var error = path.charAt(0) === "/" ? "" : "Use an absolute path or ~/: " + path
  return { path: path, caption: caption.trim(), error: error }
}
