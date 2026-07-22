-- 用法: psql -v user_id='<目标用户 UUID>' -f sql/set_admin_super_admin.sql

-- 1. 为指定用户绑定 super_admin 角色（若已存在则忽略）
INSERT INTO user_roles (user_id, role_id)
SELECT :'user_id'::uuid, id
FROM roles
WHERE code = 'super_admin'
ON CONFLICT (user_id, role_id) DO NOTHING;

-- 2. 同步 users 表的 role 字段（便于部分中间件按 role 判断）
UPDATE users
SET role = 'super_admin'
WHERE id = :'user_id'::uuid;

-- 3. 验证
SELECT u.id, u.username, u.email, u.role, r.code AS role_code, r.name AS role_name
FROM users u
LEFT JOIN user_roles ur ON u.id = ur.user_id
LEFT JOIN roles r ON ur.role_id = r.id
WHERE u.id = :'user_id'::uuid;
