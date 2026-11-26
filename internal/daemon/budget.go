package daemon

import "gitlab.com/scabb-island/omaslack/internal/ratebudget"

// Slack's documented rate-limit tiers, per method and workspace. Burst is
// ours: how many calls may go out at once after a quiet period, so opening
// a few conversations does not queue behind the per-minute rate.
var (
	tier1 = ratebudget.Limit{PerMinute: 1, Burst: 3}
	tier2 = ratebudget.Limit{PerMinute: 20, Burst: 5}
	tier3 = ratebudget.Limit{PerMinute: 50, Burst: 10}
	// chat.postMessage is "special": about one message per second per
	// channel.
	postLimit = ratebudget.Limit{PerMinute: 60, Burst: 5}
)

// slackLimits is the budget of every method the sync engine calls (spec
// sync-engine, "Rate-limit budget"). Both providers are exempt from the
// 1-call-per-minute history cap of non-Marketplace apps: an internal app
// by Slack's rules, the desktop session because it is Slack's own client.
var slackLimits = map[string]ratebudget.Limit{
	// The real-time connection: apps.connections.open (Socket Mode) and
	// the web client's socket are both tier 1.
	"connect":               tier1,
	"conversations.history": tier3,
	"conversations.replies": tier3,
	"conversations.mark":    tier3,
	"conversations.info":    tier3,
	"users.conversations":   tier3,
	"chat.postMessage":      postLimit,
	"chat.update":           tier3,
	"chat.delete":           tier3,
	"reactions.add":         tier3,
	"reactions.remove":      tier3,
	"search.messages":       tier2,
	"users.search":          tier2,
	"users.setPresence":     tier2,
	"dnd.setSnooze":         tier2,
	"dnd.endSnooze":         tier2,
}

// newBudget is one workspace's budget: Slack's limits are per workspace,
// so every workspace gets its own. Any other method gets tier 2.
func newBudget() (*ratebudget.Limiter, error) {
	return ratebudget.New(slackLimits, tier2, ratebudget.SystemClock{})
}
