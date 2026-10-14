package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTeamRole(t *testing.T) {
	t.Parallel()
	tests := []struct {
		role      TeamRole
		valid     bool
		canManage bool
	}{
		{role: TeamRoleOwner, valid: true, canManage: true},
		{role: TeamRoleAdmin, valid: true, canManage: true},
		{role: TeamRoleMember, valid: true},
		{role: ""},
		{role: "superuser"},
	}
	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.valid, tt.role.Valid())
			assert.Equal(t, tt.canManage, tt.role.CanManageMembers())
		})
	}
}
