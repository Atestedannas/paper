package database

// ResetOrdersMemberLevel 重置 orders 表的 member_level_id 字段
//
// 注意：这个函数在 main.go 里被调用于 PerformMigration 之前，而空库上
// orders 表还不存在。此时应视为「无需修复」直接返回 nil，而不是报错——
// 否则全新环境启动会直接 log.Fatalf 退出。
func ResetOrdersMemberLevel() error {
	if !DB.Migrator().HasTable("orders") {
		return nil
	}

	// 删除外键约束
	if err := DB.Exec("ALTER TABLE orders DROP CONSTRAINT IF EXISTS fk_orders_member_level").Error; err != nil {
		return err
	}

	// 将 member_level_id 改为可空
	if err := DB.Exec("ALTER TABLE orders ALTER COLUMN member_level_id DROP NOT NULL").Error; err != nil {
		return err
	}

	return nil
}
