package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedeemServiceGenerateRandomCodeFitsRedeemCodeColumn(t *testing.T) {
	svc := &RedeemService{}

	code, err := svc.GenerateRandomCode()

	require.NoError(t, err)
	require.Len(t, code, 32)
	for _, ch := range code {
		require.Truef(t, (ch >= '0' && ch <= '9') || (ch >= 'A' && ch <= 'F'), "unexpected code character %q in %q", ch, code)
	}
}
