-- 为 super_admin 角色分配全部菜单（解决超级管理员侧栏只显示部分菜单的问题）
-- 执行后需重新登录或刷新页面以重新拉取用户菜单树

INSERT INTO role_menus (role_id, menu_id)
SELECT r.id, m.id
FROM roles r
CROSS JOIN menus m
WHERE r.code = 'super_admin'
ON CONFLICT (role_id, menu_id) DO NOTHING;
