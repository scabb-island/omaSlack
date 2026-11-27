package render

import "strings"

// StandardEmoji resolves a standard Slack shortcode (no colons) to its
// Unicode emoji. The table covers the shortcodes people use most; anything
// else goes to Directory.Emoji. The app can use it for its own Directory and
// for the reaction picker.
func StandardEmoji(name string) (string, bool) {
	s, ok := standardEmoji[name]
	return s, ok
}

// emojiText is what render prints for a shortcode or a reaction name such
// as "+1::skin-tone-2": standard first, then the Directory (custom), and
// ok=false when neither knows a part.
func emojiText(name string, d Directory) (string, bool) {
	var b strings.Builder
	for part := range strings.SplitSeq(name, "::") {
		s, ok := StandardEmoji(part)
		if !ok {
			s, ok = d.Emoji(part)
		}
		if !ok {
			return "", false
		}
		b.WriteString(clean(s))
	}
	return b.String(), true
}

// standardEmoji maps Slack shortcodes to Unicode. Skin tones are modifiers
// that join the emoji right before them, as ":wave::skin-tone-3:" does.
var standardEmoji = map[string]string{
	"skin-tone-2": "\U0001F3FB", "skin-tone-3": "\U0001F3FC", "skin-tone-4": "\U0001F3FD",
	"skin-tone-5": "\U0001F3FE", "skin-tone-6": "\U0001F3FF",

	// Faces.
	"smile": "😄", "smiley": "😃", "grinning": "😀", "grin": "😁", "laughing": "😆", "satisfied": "😆",
	"sweat_smile": "😅", "joy": "😂", "rolling_on_the_floor_laughing": "🤣", "slightly_smiling_face": "🙂",
	"upside_down_face": "🙃", "wink": "😉", "blush": "😊", "innocent": "😇", "heart_eyes": "😍",
	"star-struck": "🤩", "kissing_heart": "😘", "yum": "😋", "stuck_out_tongue": "😛",
	"stuck_out_tongue_winking_eye": "😜", "zany_face": "🤪", "money_mouth_face": "🤑", "hugging_face": "🤗",
	"thinking_face": "🤔", "thinking": "🤔", "shushing_face": "🤫", "zipper_mouth_face": "🤐",
	"raised_eyebrow": "🤨", "neutral_face": "😐", "expressionless": "😑", "no_mouth": "😶",
	"smirk": "😏", "unamused": "😒", "face_with_rolling_eyes": "🙄", "roll_eyes": "🙄", "grimacing": "😬",
	"relieved": "😌", "pensive": "😔", "sleepy": "😪", "sleeping": "😴", "mask": "😷",
	"face_with_thermometer": "🤒", "nauseated_face": "🤢", "sneezing_face": "🤧", "hot_face": "🥵",
	"cold_face": "🥶", "dizzy_face": "😵", "exploding_head": "🤯", "cowboy_hat_face": "🤠",
	"partying_face": "🥳", "sunglasses": "😎", "nerd_face": "🤓", "confused": "😕", "worried": "😟",
	"slightly_frowning_face": "🙁", "open_mouth": "😮", "hushed": "😯", "astonished": "😲",
	"flushed": "😳", "pleading_face": "🥺", "fearful": "😨", "cold_sweat": "😰", "cry": "😢",
	"sob": "😭", "scream": "😱", "confounded": "😖", "persevere": "😣", "disappointed": "😞",
	"sweat": "😓", "weary": "😩", "tired_face": "😫", "yawning_face": "🥱", "triumph": "😤",
	"rage": "😡", "angry": "😠", "smiling_imp": "😈", "skull": "💀", "poop": "💩", "hankey": "💩",
	"clown_face": "🤡", "ghost": "👻", "alien": "👽", "robot_face": "🤖", "smile_cat": "😸",
	"see_no_evil": "🙈", "hear_no_evil": "🙉", "speak_no_evil": "🙊", "melting_face": "🫠",
	"saluting_face": "🫡", "face_with_spiral_eyes": "😵‍💫", "sweat_drops": "💦",

	// Hands and people.
	"+1": "👍", "thumbsup": "👍", "-1": "👎", "thumbsdown": "👎", "ok_hand": "👌", "wave": "👋",
	"clap": "👏", "raised_hands": "🙌", "pray": "🙏", "handshake": "🤝", "muscle": "💪",
	"point_up": "☝️", "point_down": "👇", "point_left": "👈", "point_right": "👉", "v": "✌️",
	"crossed_fingers": "🤞", "the_horns": "🤘", "call_me_hand": "🤙", "fist": "✊", "facepunch": "👊",
	"punch": "👊", "raised_hand": "✋", "hand": "✋", "open_hands": "👐", "writing_hand": "✍️",
	"eyes": "👀", "eye": "👁️", "brain": "🧠", "shrug": "🤷", "facepalm": "🤦", "bow": "🙇",
	"man-shrugging": "🤷‍♂️", "woman-shrugging": "🤷‍♀️", "dancer": "💃", "runner": "🏃",

	// Hearts and symbols.
	"heart": "❤️", "orange_heart": "🧡", "yellow_heart": "💛", "green_heart": "💚",
	"blue_heart": "💙", "purple_heart": "💜", "black_heart": "🖤", "white_heart": "🤍",
	"broken_heart": "💔", "two_hearts": "💕", "sparkling_heart": "💖", "100": "💯", "boom": "💥",
	"collision": "💥", "anger": "💢", "dizzy": "💫", "zzz": "💤", "speech_balloon": "💬",
	"thought_balloon": "💭", "white_check_mark": "✅", "heavy_check_mark": "✔️", "ballot_box_with_check": "☑️",
	"x": "❌", "negative_squared_cross_mark": "❎", "heavy_plus_sign": "➕", "heavy_minus_sign": "➖",
	"question": "❓", "grey_question": "❔", "exclamation": "❗", "heavy_exclamation_mark": "❗",
	"bangbang": "‼️", "interrobang": "⁉️", "warning": "⚠️", "no_entry": "⛔", "no_entry_sign": "🚫",
	"red_circle": "🔴", "large_orange_circle": "🟠", "large_yellow_circle": "🟡", "large_green_circle": "🟢",
	"large_blue_circle": "🔵", "white_circle": "⚪", "black_circle": "⚫", "arrow_up": "⬆️",
	"arrow_down": "⬇️", "arrow_left": "⬅️", "arrow_right": "➡️", "arrows_counterclockwise": "🔄",
	"repeat": "🔁", "recycle": "♻️", "copyright": "©️", "registered": "®️", "tm": "™️",
	"information_source": "ℹ️", "new": "🆕", "free": "🆓", "up": "🆙", "cool": "🆒", "ok": "🆗",
	"sos": "🆘", "hash": "#️⃣", "zero": "0️⃣", "one": "1️⃣", "two": "2️⃣", "three": "3️⃣",
	"four": "4️⃣", "five": "5️⃣", "six": "6️⃣", "seven": "7️⃣", "eight": "8️⃣", "nine": "9️⃣",
	"keycap_ten": "🔟",

	// Nature, food and weather.
	"fire": "🔥", "sparkles": "✨", "star": "⭐", "star2": "🌟", "sunny": "☀️", "cloud": "☁️",
	"umbrella": "☔", "snowflake": "❄️", "zap": "⚡", "rainbow": "🌈", "ocean": "🌊", "droplet": "💧",
	"seedling": "🌱", "evergreen_tree": "🌲", "deciduous_tree": "🌳", "cactus": "🌵", "rose": "🌹",
	"sunflower": "🌻", "tulip": "🌷", "four_leaf_clover": "🍀", "fallen_leaf": "🍂", "earth_americas": "🌎",
	"new_moon": "🌑", "full_moon": "🌕", "crescent_moon": "🌙", "dog": "🐶", "cat": "🐱", "mouse": "🐭",
	"rabbit": "🐰", "fox_face": "🦊", "bear": "🐻", "panda_face": "🐼", "unicorn_face": "🦄",
	"bee": "🐝", "bug": "🐛", "butterfly": "🦋", "snail": "🐌", "turtle": "🐢", "snake": "🐍",
	"octopus": "🐙", "whale": "🐳", "penguin": "🐧", "chicken": "🐔", "owl": "🦉", "parrot": "🦜",
	"apple": "🍎", "lemon": "🍋", "banana": "🍌", "watermelon": "🍉", "grapes": "🍇",
	"strawberry": "🍓", "peach": "🍑", "avocado": "🥑", "hot_pepper": "🌶️", "corn": "🌽",
	"bread": "🍞", "cheese_wedge": "🧀", "hamburger": "🍔", "fries": "🍟", "pizza": "🍕",
	"hotdog": "🌭", "taco": "🌮", "burrito": "🌯", "sushi": "🍣", "ramen": "🍜", "spaghetti": "🍝",
	"cake": "🍰", "birthday": "🎂", "cookie": "🍪", "doughnut": "🍩", "chocolate_bar": "🍫",
	"popcorn": "🍿", "coffee": "☕", "tea": "🍵", "beer": "🍺", "beers": "🍻", "wine_glass": "🍷",
	"cocktail": "🍸", "tropical_drink": "🍹", "champagne": "🍾", "clinking_glasses": "🥂",
	"mate_drink": "🧉",

	// Activities, objects and travel.
	"tada": "🎉", "confetti_ball": "🎊", "balloon": "🎈", "gift": "🎁", "trophy": "🏆",
	"medal": "🏅", "first_place_medal": "🥇", "soccer": "⚽", "basketball": "🏀", "football": "🏈",
	"tennis": "🎾", "video_game": "🎮", "game_die": "🎲", "dart": "🎯", "musical_note": "🎵",
	"notes": "🎶", "headphones": "🎧", "microphone": "🎤", "guitar": "🎸", "art": "🎨",
	"movie_camera": "🎥", "camera": "📷", "tv": "📺", "computer": "💻", "keyboard": "⌨️",
	"desktop_computer": "🖥️", "iphone": "📱", "phone": "☎️", "telephone_receiver": "📞",
	"battery": "🔋", "electric_plug": "🔌", "bulb": "💡", "flashlight": "🔦", "wrench": "🔧",
	"hammer": "🔨", "hammer_and_wrench": "🛠️", "gear": "⚙️", "nut_and_bolt": "🔩", "link": "🔗",
	"paperclip": "📎", "pushpin": "📌", "round_pushpin": "📍", "scissors": "✂️", "lock": "🔒",
	"unlock": "🔓", "key": "🔑", "mag": "🔍", "mag_right": "🔎", "bell": "🔔", "no_bell": "🔕",
	"loudspeaker": "📢", "mega": "📣", "email": "📧", "envelope": "✉️", "inbox_tray": "📥",
	"outbox_tray": "📤", "package": "📦", "memo": "📝", "pencil": "📝", "pencil2": "✏️",
	"book": "📖", "books": "📚", "bookmark": "🔖", "clipboard": "📋", "calendar": "📆",
	"date": "📅", "chart_with_upwards_trend": "📈", "chart_with_downwards_trend": "📉",
	"bar_chart": "📊", "file_folder": "📁", "open_file_folder": "📂", "page_facing_up": "📄",
	"wastebasket": "🗑️", "hourglass": "⌛", "hourglass_flowing_sand": "⏳", "stopwatch": "⏱️",
	"alarm_clock": "⏰", "watch": "⌚", "moneybag": "💰", "dollar": "💵", "credit_card": "💳",
	"gem": "💎", "rocket": "🚀", "airplane": "✈️", "car": "🚗", "taxi": "🚕", "bus": "🚌",
	"bike": "🚲", "ship": "🚢", "construction": "🚧", "rotating_light": "🚨", "checkered_flag": "🏁",
	"triangular_flag_on_post": "🚩", "house": "🏠", "office": "🏢", "hospital": "🏥",
	"globe_with_meridians": "🌐", "crystal_ball": "🔮", "magic_wand": "🪄", "pill": "💊",
	"syringe": "💉", "dna": "🧬", "microscope": "🔬", "telescope": "🔭", "test_tube": "🧪",
	"label": "🏷️", "shield": "🛡️", "crown": "👑", "eyeglasses": "👓", "tophat": "🎩",
	"ticket": "🎫", "flag-ar": "🇦🇷", "flag-us": "🇺🇸", "flag-gb": "🇬🇧", "flag-es": "🇪🇸",
	"flag-de": "🇩🇪", "flag-fr": "🇫🇷", "flag-br": "🇧🇷", "flag-jp": "🇯🇵",
}
