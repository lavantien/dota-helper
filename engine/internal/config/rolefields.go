package config

import "fmt"

// validateSharedRolePools: group members must be real role ids, groups need
// at least two roles, and no role may sit in two groups (a role's candidate
// field would otherwise depend on which group answered first).
func (c *Config) validateSharedRolePools(roleIDs map[string]bool) error {
	seen := map[string]bool{}
	for _, g := range c.SharedRolePools {
		if len(g) < 2 {
			return fmt.Errorf("sharedRolePools group %v needs at least two roles", g)
		}
		for _, r := range g {
			if !roleIDs[r] {
				return fmt.Errorf("sharedRolePools references unknown role %q", r)
			}
			if seen[r] {
				return fmt.Errorf("sharedRolePools lists role %q in more than one group", r)
			}
			seen[r] = true
		}
	}
	return nil
}

// RoleField returns the candidate field of role: the seat itself plus the
// other seats of its shared pool. Candidate membership and gate scoping both
// key on it, so the go engine and the picker page stay symmetric.
func (c *Config) RoleField(role string) []string {
	for _, g := range c.SharedRolePools {
		for _, r := range g {
			if r == role {
				return g
			}
		}
	}
	return []string{role}
}

// PlaysRole reports whether a hero fielded at roles is a candidate for role:
// exact membership, or any seat shared between them.
func (c *Config) PlaysRole(roles []string, role string) bool {
	field := c.RoleField(role)
	for _, r := range roles {
		for _, f := range field {
			if r == f {
				return true
			}
		}
	}
	return false
}
