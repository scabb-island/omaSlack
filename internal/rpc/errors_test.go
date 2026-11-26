package rpc

import (
	"errors"
	"fmt"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

// The codes are part of the protocol (spec daemon-api); changing one breaks
// every client.
func TestErrorCodesMatchTheProtocol(t *testing.T) {
	want := map[Code]int{
		CodeParseError:            -32700,
		CodeInvalidRequest:        -32600,
		CodeMethodNotFound:        -32601,
		CodeInvalidParams:         -32602,
		CodeInternalError:         -32603,
		CodeIncompatibleProtocol:  -32001,
		CodeUnsupportedCapability: -32002,
		CodeAuthExpired:           -32003,
	}
	for code, n := range want {
		if int(code) != n {
			t.Errorf("code %v = %d, want %d", code, int(code), n)
		}
	}
}

func TestErrorMessage(t *testing.T) {
	err := &Error{Code: CodeMethodNotFound, Message: "Method not found"}
	if err.Error() != "rpc error -32601: Method not found" {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestUnsupportedCapabilityMapsToItsCode(t *testing.T) {
	cause := domain.NewCapabilities().Require(domain.Search)
	got := FromError(cause)
	if got.Code != CodeUnsupportedCapability {
		t.Errorf("code = %d, want %d", got.Code, CodeUnsupportedCapability)
	}
	if got.Message != cause.Error() {
		t.Errorf("message = %q, want %q", got.Message, cause.Error())
	}
}

// Spec daemon-api lists -32003 auth_expired: a client must tell "sign in
// again" apart from any other failure, however deep the provider wrapped it.
func TestExpiredCredentialsMapToAuthExpired(t *testing.T) {
	cause := fmt.Errorf("syncengine: send: %w", fmt.Errorf("chat.postMessage: invalid_auth: %w", provider.ErrAuthExpired))
	got := FromError(cause)
	if got.Code != CodeAuthExpired || got.Message != "auth_expired" {
		t.Errorf("FromError = %+v, want -32003 auth_expired", got)
	}
}

func TestRPCErrorsPassThroughAndOthersAreInternal(t *testing.T) {
	own := &Error{Code: CodeInvalidParams, Message: "bad team"}
	if got := FromError(own); got != own {
		t.Errorf("an *Error was rewrapped: %+v", got)
	}
	got := FromError(errors.New("disk on fire"))
	if got.Code != CodeInternalError || got.Message != "internal error" {
		t.Errorf("a plain error became %+v, want an internal error that hides its text", got)
	}
}
