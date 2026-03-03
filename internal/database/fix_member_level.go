package database

// ResetOrdersMemberLevel 重置 orders 表的 member_level_id 字段
func ResetOrdersMemberLevel() error {
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
