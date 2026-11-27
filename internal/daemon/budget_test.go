package daemon

import (
	"context"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
	"gitlab.com/scabb-island/omaslack/internal/ratebudget"
)

func TestTheBudgetCoversTheMethodsTheEngineCalls(t *testing.T) {
	for _, m := range []string{
		providertest.MethodConnect, providertest.MethodHistory, providertest.MethodReplies,
		providertest.MethodPost, providertest.MethodMarkRead, providertest.MethodReadMarker,
		providertest.MethodConversations,
		"users.info", "emoji.list", "typing", // the client actions of task 3.8
	} {
		if _, ok := slackLimits[m]; !ok {
			t.Errorf("no limit for %s", m)
		}
	}
}

func TestTheBudgetLetsABurstThroughAtOnce(t *testing.T) {
	b, err := newBudget()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for i := range slackLimits[providertest.MethodHistory].Burst {
		if err := b.Wait(ctx, providertest.MethodHistory, ratebudget.Interactive); err != nil {
			t.Fatalf("call %d waited: %v", i, err)
		}
	}
}
