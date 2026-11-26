package domain

import (
	"errors"
	"testing"
)

func TestParseFileID(t *testing.T) {
	id, err := ParseFileID("F0123ABCD")
	if err != nil || id.String() != "F0123ABCD" || id.IsZero() {
		t.Fatalf("ParseFileID = (%q, %v)", id, err)
	}
	for _, bad := range []string{"", "F", "C0123ABCD", "F0123abcd", "F0/../x"} {
		if id, err := ParseFileID(bad); !errors.Is(err, ErrInvalidID) || !id.IsZero() {
			t.Errorf("ParseFileID(%q) = (%q, %v), want ErrInvalidID", bad, id, err)
		}
	}
}
