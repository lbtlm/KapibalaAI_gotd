package gateway

import (
	"github.com/gotd/td/tgerr"
	"testing"
)

func TestGroupAddressErrorsRemainActionable(t *testing.T) {
	for _, code := range []string{"USERNAME_OCCUPIED", "USERNAME_INVALID", "USERNAME_PURCHASE_AVAILABLE", "CHANNELS_ADMIN_PUBLIC_TOO_MUCH"} {
		err := platformError(&tgerr.Error{Code: 400, Type: code})
		if err.Code != code || err.Status != 422 {
			t.Fatalf("address cause lost: %+v", err)
		}
	}
}
