-- 将 test@qq.com 设置为超级管理员

-- 1. 查找 test@qq.com 用户的 ID
SELECT id, email, name FROM users WHERE email = 'test@qq.com';

-- 2. 查找超级管理员角色的 ID
SELECT id, name, code FROM roles WHERE code = 'super_admin';

-- 3. 为用户分配超级管理员角色
INSERT INTO user_roles (user_id, role_id)
SELECT u.id, r.id
FROM users u, roles r
WHERE u.email = 'test@qq.com' AND r.code = 'super_admin';

-- 4. 验证分配结果
SELECT u.email, r.name as role_name, r.code as role_code
FROM users u
JOIN user_roles ur ON u.id = ur.user_id
JOIN roles r ON ur.role_id = r.id
WHERE u.email = 'test@qq.com';
