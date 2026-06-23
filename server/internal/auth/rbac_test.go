package auth

import "testing"

// Trava o contrato RBAC do qual dependem os guards HTTP (requireWriter/requireAdmin)
// e o gating de ACL: admin escreve+administra, operator escreve, viewer só lê.
func TestRoleCapabilityMatrix(t *testing.T) {
	cases := []struct {
		role                Role
		valid, write, admin bool
	}{
		{RoleAdmin, true, true, true},
		{RoleOperator, true, true, false},
		{RoleViewer, true, false, false},
		{Role("bogus"), false, false, false},
		{Role(""), false, false, false},
	}
	for _, c := range cases {
		if got := c.role.Valid(); got != c.valid {
			t.Errorf("%q.Valid()=%v, quer %v", c.role, got, c.valid)
		}
		if got := c.role.CanWrite(); got != c.write {
			t.Errorf("%q.CanWrite()=%v, quer %v", c.role, got, c.write)
		}
		if got := c.role.CanAdmin(); got != c.admin {
			t.Errorf("%q.CanAdmin()=%v, quer %v", c.role, got, c.admin)
		}
	}
}
