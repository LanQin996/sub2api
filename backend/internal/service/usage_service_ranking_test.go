package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicRankingDisplayName(t *testing.T) {
	require.Equal(t, "alice", publicRankingDisplayName(1, " alice ", "alice@example.com"))
	require.Equal(t, "a***e@example.com", publicRankingDisplayName(2, "", "alice@example.com"))
	require.Equal(t, "User #3", publicRankingDisplayName(3, "", ""))
}
