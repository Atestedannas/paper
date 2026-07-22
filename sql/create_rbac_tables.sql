-- 创建 RBAC 相关表
-- 执行此脚本前请确保已连接到正确的数据库

-- 1. 创建角色表
CREATE TABLE IF NOT EXISTS roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(50) NOT NULL UNIQUE,
    description VARCHAR(200),
    type VARCHAR(20) DEFAULT 'business',
    parent_id UUID REFERENCES roles(id) ON DELETE SET NULL,
    code VARCHAR(50) NOT NULL UNIQUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 2. 创建权限表
CREATE TABLE IF NOT EXISTS permissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    code VARCHAR(100) NOT NULL UNIQUE,
    resource_type VARCHAR(50) DEFAULT 'api',
    method VARCHAR(10) DEFAULT 'GET',
    path VARCHAR(200),
    description VARCHAR(200),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 3. 创建用户角色关联表
CREATE TABLE IF NOT EXISTS user_roles (
    user_id UUID NOT NULL,
    role_id UUID NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, role_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
);

-- 4. 创建用户权限关联表（直接分配给用户的额外权限）
CREATE TABLE IF NOT EXISTS user_permissions (
    user_id UUID NOT NULL,
    permission_id UUID NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, permission_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELETE CASCADE
);

-- 5. 创建角色权限关联表
CREATE TABLE IF NOT EXISTS role_permissions (
    role_id UUID NOT NULL,
    permission_id UUID NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (role_id, permission_id),
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE,
    FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELETE CASCADE
);

-- 创建索引以提高查询性能
CREATE INDEX IF NOT EXISTS idx_user_roles_role_id ON user_roles(role_id);
CREATE INDEX IF NOT EXISTS idx_user_permissions_user_id ON user_permissions(user_id);
CREATE INDEX IF NOT EXISTS idx_user_permissions_permission_id ON user_permissions(permission_id);
CREATE INDEX IF NOT EXISTS idx_role_permissions_role_id ON role_permissions(role_id);
CREATE INDEX IF NOT EXISTS idx_role_permissions_permission_id ON role_permissions(permission_id);
CREATE INDEX IF NOT EXISTS idx_permissions_code ON permissions(code);
CREATE INDEX IF NOT EXISTS idx_roles_code ON roles(code);

-- 插入默认角色数据
INSERT INTO roles (name, description, type, code) 
VALUES 
    ('超级管理员', '拥有系统最高权限的管理员角色', 'system', 'super_admin'),
    ('系统管理员', '系统管理员，拥有大部分管理权限', 'system', 'admin'),
    ('普通用户', '普通用户，拥有基本使用权限', 'system', 'user'),
    ('审核员', '负责审核内容的用户角色', 'business', 'reviewer'),
    ('财务人员', '负责财务管理的用户角色', 'business', 'finance')
ON CONFLICT (code) DO NOTHING;

-- 插入默认权限数据
INSERT INTO permissions (name, code, resource_type, method, path, description) 
VALUES 
    -- 用户管理权限
    ('用户列表查看', 'user:list', 'api', 'GET', '/api/v1/admin/users', '允许查看用户列表'),
    ('用户角色更新', 'user:update_role', 'api', 'PUT', '/api/v1/admin/users/*/role', '允许更新用户角色'),
    ('用户状态更新', 'user:update_status', 'api', 'PUT', '/api/v1/admin/users/*/status', '允许更新用户状态'),
    ('用户删除', 'user:delete', 'api', 'DELETE', '/api/v1/admin/users/*', '允许删除用户'),
    
    -- 论文管理权限
    ('论文列表查看', 'paper:list', 'api', 'GET', '/api/v1/admin/papers', '允许查看论文列表'),
    ('论文详情查看', 'paper:read', 'api', 'GET', '/api/v1/papers/*', '允许查看论文详情'),
    ('论文上传', 'paper:create', 'api', 'POST', '/api/v1/papers/upload', '允许上传论文'),
    ('论文格式检查', 'paper:check', 'api', 'POST', '/api/v1/papers/*/check-format', '允许检查论文格式'),
    ('论文格式修复', 'paper:fix', 'api', 'POST', '/api/v1/papers/*/apply-corrections', '允许修复论文格式'),
    ('论文格式管理', 'paper:format:manage', 'api', '*', '/api/v1/admin/papers/format*', '允许管理论文格式'),
    
    -- 订单管理权限
    ('订单列表查看', 'order:list', 'api', 'GET', '/api/v1/admin/orders', '允许查看订单列表'),
    ('订单状态更新', 'order:update_status', 'api', 'PUT', '/api/v1/admin/orders/*/status', '允许更新订单状态'),
    
    -- 系统管理权限
    ('系统配置查看', 'system:config:read', 'api', 'GET', '/api/v1/admin/settings/*', '允许查看系统配置'),
    ('系统配置更新', 'system:config:update', 'api', 'PUT', '/api/v1/admin/settings/*', '允许更新系统配置'),
    
    -- RBAC管理权限
    ('角色管理', 'rbac:role:manage', 'api', '*', '/api/v1/admin/roles/*', '允许管理角色'),
    ('权限管理', 'rbac:permission:manage', 'api', '*', '/api/v1/admin/permissions/*', '允许管理权限')
ON CONFLICT (code) DO NOTHING;

-- 为超级管理员分配所有权限
DO $$
DECLARE
    super_admin_role_id UUID;
    perm_id UUID;
BEGIN
    SELECT id INTO super_admin_role_id FROM roles WHERE code = 'super_admin' LIMIT 1;
    
    IF super_admin_role_id IS NOT NULL THEN
        FOR perm_id IN SELECT id FROM permissions LOOP
            INSERT INTO role_permissions (role_id, permission_id)
            VALUES (super_admin_role_id, perm_id)
            ON CONFLICT DO NOTHING;
        END LOOP;
    END IF;
END $$;

-- 为管理员分配常用权限
DO $$
DECLARE
    admin_role_id UUID;
    perm_code TEXT;
    perm_id UUID;
BEGIN
    SELECT id INTO admin_role_id FROM roles WHERE code = 'admin' LIMIT 1;
    
    IF admin_role_id IS NOT NULL THEN
        FOREACH perm_code IN ARRAY ARRAY[
            'user:list', 'user:update_role', 'user:update_status', 'user:delete',
            'paper:list', 'paper:read', 'paper:create', 'paper:check', 'paper:fix',
            'order:list', 'order:update_status',
            'system:config:read', 'system:config:update'
        ] LOOP
            SELECT id INTO perm_id FROM permissions WHERE code = perm_code LIMIT 1;
            IF perm_id IS NOT NULL THEN
                INSERT INTO role_permissions (role_id, permission_id)
                VALUES (admin_role_id, perm_id)
                ON CONFLICT DO NOTHING;
            END IF;
        END LOOP;
    END IF;
END $$;

-- 为审核员分配相关权限
DO $$
DECLARE
    reviewer_role_id UUID;
    perm_code TEXT;
    perm_id UUID;
BEGIN
    SELECT id INTO reviewer_role_id FROM roles WHERE code = 'reviewer' LIMIT 1;
    
    IF reviewer_role_id IS NOT NULL THEN
        FOREACH perm_code IN ARRAY ARRAY['paper:list', 'paper:read', 'paper:check'] LOOP
            SELECT id INTO perm_id FROM permissions WHERE code = perm_code LIMIT 1;
            IF perm_id IS NOT NULL THEN
                INSERT INTO role_permissions (role_id, permission_id)
                VALUES (reviewer_role_id, perm_id)
                ON CONFLICT DO NOTHING;
            END IF;
        END LOOP;
    END IF;
END $$;

-- 为财务人员分配相关权限
DO $$
DECLARE
    finance_role_id UUID;
    perm_code TEXT;
    perm_id UUID;
BEGIN
    SELECT id INTO finance_role_id FROM roles WHERE code = 'finance' LIMIT 1;
    
    IF finance_role_id IS NOT NULL THEN
        FOREACH perm_code IN ARRAY ARRAY['order:list', 'order:update_status'] LOOP
            SELECT id INTO perm_id FROM permissions WHERE code = perm_code LIMIT 1;
            IF perm_id IS NOT NULL THEN
                INSERT INTO role_permissions (role_id, permission_id)
                VALUES (finance_role_id, perm_id)
                ON CONFLICT DO NOTHING;
            END IF;
        END LOOP;
    END IF;
END $$;
