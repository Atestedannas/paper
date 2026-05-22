package database

import "gorm.io/gorm"

type RBACHealthStatus struct {
	DatabaseConnected      bool   `json:"database_connected"`
	RBACModel              string `json:"rbac_model"`
	PermissionsCount       int64  `json:"permissions_count"`
	RolePermissionsCount   int64  `json:"role_permissions_count"`
	AuthoritiesCount       int64  `json:"authorities_count"`
	RoleAuthoritiesCount   int64  `json:"role_authorities_count"`
	AuthorityOnlyCodes     int64  `json:"authority_only_codes"`
	PermissionOnlyCodes    int64  `json:"permission_only_codes"`
	AuthorityRoleOrphans   int64  `json:"authority_role_orphans"`
	LegacyTablesPresent    bool   `json:"legacy_tables_present"`
	CanonicalTablesPresent bool   `json:"canonical_tables_present"`
	LegacyDecommissionReady bool   `json:"legacy_decommission_ready"`
	LegacyDecommissionBlockers []string `json:"legacy_decommission_blockers"`
}

func tableCount(db *gorm.DB, table string) int64 {
	var count int64
	if db.Migrator().HasTable(table) {
		_ = db.Table(table).Count(&count).Error
	}
	return count
}

func codeDiffCount(db *gorm.DB, leftTable, rightTable string) int64 {
	var count int64
	if !db.Migrator().HasTable(leftTable) || !db.Migrator().HasTable(rightTable) {
		return 0
	}
	_ = db.Raw(
		`SELECT COUNT(*) FROM `+leftTable+` l WHERE NOT EXISTS (SELECT 1 FROM `+rightTable+` r WHERE r.code = l.code)`,
	).Scan(&count).Error
	return count
}

func GetRBACHealthStatus(model string) RBACHealthStatus {
	status := RBACHealthStatus{
		RBACModel: model,
	}
	if DB == nil {
		return status
	}

	status.DatabaseConnected = true
	status.LegacyTablesPresent = DB.Migrator().HasTable("authorities") && DB.Migrator().HasTable("role_authorities")
	status.CanonicalTablesPresent = DB.Migrator().HasTable("permissions") && DB.Migrator().HasTable("role_permissions")

	status.PermissionsCount = tableCount(DB, "permissions")
	status.RolePermissionsCount = tableCount(DB, "role_permissions")
	status.AuthoritiesCount = tableCount(DB, "authorities")
	status.RoleAuthoritiesCount = tableCount(DB, "role_authorities")
	status.AuthorityOnlyCodes = codeDiffCount(DB, "authorities", "permissions")
	status.PermissionOnlyCodes = codeDiffCount(DB, "permissions", "authorities")
	status.AuthorityRoleOrphans = authorityRoleOrphansCount(DB)

	blockers := make([]string, 0)
	if !status.CanonicalTablesPresent {
		blockers = append(blockers, "canonical tables missing: permissions/role_permissions")
	}
	if status.LegacyTablesPresent && status.AuthorityOnlyCodes > 0 {
		blockers = append(blockers, "authorities has codes not migrated to permissions")
	}
	if status.LegacyTablesPresent && status.AuthorityRoleOrphans > 0 {
		blockers = append(blockers, "role_authorities has mappings not representable in role_permissions")
	}
	status.LegacyDecommissionBlockers = blockers
	status.LegacyDecommissionReady = len(blockers) == 0

	return status
}

func authorityRoleOrphansCount(db *gorm.DB) int64 {
	var count int64
	if !db.Migrator().HasTable("role_authorities") || !db.Migrator().HasTable("authorities") || !db.Migrator().HasTable("permissions") {
		return 0
	}
	_ = db.Raw(`
		SELECT COUNT(*)
		FROM role_authorities ra
		JOIN authorities a ON a.id = ra.authority_id
		LEFT JOIN permissions p ON p.code = a.code
		WHERE p.id IS NULL
	`).Scan(&count).Error
	return count
}

