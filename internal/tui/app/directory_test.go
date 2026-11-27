package app

import "testing"

func TestDirectorySelfComesFromHello(t *testing.T) {
	s, err := NewState([]byte(`{"protocol":1,"workspaces":[` +
		`{"team":"T1","name":"Acme","self":"U0ME","provider":"internal-app","capabilities":[],"state":"connected"},` +
		`{"team":"T2","name":"Beta","provider":"internal-app","capabilities":[],"state":"connected"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	for team, want := range map[string]string{"T1": "U0ME", "T2": "", "T9": ""} {
		if got := (directory{st: s, team: team}).Self(); got != want {
			t.Errorf("Self() of %s = %q, want %q", team, got, want)
		}
	}
}
