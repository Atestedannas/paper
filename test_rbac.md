# RBAC 功能测试指南

## 后端服务已启动

服务器地址：http://localhost:8080

## 测试步骤

### 1. 登录获取 Token

首先需要登录获取管理员 token：

```bash
POST http://localhost:8080/api/v1/auth/login
Content-Type: application/json

{
  "email": "admin@example.com",
  "password": "admin123"
}
```

保存返回的 token 用于后续请求。

### 2. 测试菜单管理功能

#### 获取所有菜单
```bash
GET http://localhost:8080/api/v1/admin/menus
Authorization: Bearer <your_token>
```

#### 获取菜单树
```bash
GET http://localhost:8080/api/v1/admin/menus/tree
Authorization: Bearer <your_token>
```

#### 获取用户菜单
```bash
GET http://localhost:8080/api/v1/admin/menus/user
Authorization: Bearer <your_token>
```

#### 创建菜单
```bash
POST http://localhost:8080/api/v1/admin/menus
Authorization: Bearer <your_token>
Content-Type: application/json

{
  "name": "test-menu",
  "title": "测试菜单",
  "icon": "Folder",
  "path": "/admin/test",
  "component": "admin/TestView.vue",
  "menu_type": "menu",
  "permission": "test:view",
  "visible": true,
  "sort_order": 100,
  "meta": {
    "title": "测试菜单",
    "icon": "Folder"
  }
}
```

### 3. 测试权限管理功能

#### 获取所有权限
```bash
GET http://localhost:8080/api/v1/admin/authorities
Authorization: Bearer <your_token>
```

#### 创建权限
```bash
POST http://localhost:8080/api/v1/admin/authorities
Authorization: Bearer <your_token>
Content-Type: application/json

{
  "name": "测试权限",
  "code": "test:permission",
  "type": "api",
  "resource_path": "/api/v1/test",
  "http_method": "GET",
  "description": "这是一个测试权限"
}
```

### 4. 测试 Casbin 策略管理

#### 权限检查
```bash
POST http://localhost:8080/api/v1/admin/casbin/enforce
Authorization: Bearer <your_token>
Content-Type: application/json

{
  "sub": "user_id",
  "obj": "/api/v1/admin/menus",
  "act": "GET"
}
```

#### 获取用户权限
```bash
GET http://localhost:8080/api/v1/admin/casbin/user/permissions?user=<user_id>
Authorization: Bearer <your_token>
```

#### 获取用户角色
```bash
GET http://localhost:8080/api/v1/admin/casbin/user/roles?user=<user_id>
Authorization: Bearer <your_token>
```

### 5. 测试论文删除功能

#### 删除单个论文
```bash
DELETE http://localhost:8080/api/v1/admin/papers/<paper_id>
Authorization: Bearer <your_token>
```

#### 批量删除论文
```bash
POST http://localhost:8080/api/v1/admin/papers/batch-delete
Authorization: Bearer <your_token>
Content-Type: application/json

{
  "ids": ["paper_id_1", "paper_id_2", "paper_id_3"]
}
```

### 6. 测试角色菜单分配

#### 为角色分配菜单
```bash
POST http://localhost:8080/api/v1/admin/roles/<role_id>/menus
Authorization: Bearer <your_token>
Content-Type: application/json

{
  "menu_ids": ["menu_id_1", "menu_id_2", "menu_id_3"]
}
```

#### 获取角色菜单
```bash
GET http://localhost:8080/api/v1/admin/roles/<role_id>/menus
Authorization: Bearer <your_token>
```

### 7. 测试角色权限分配

#### 为角色分配权限
```bash
POST http://localhost:8080/api/v1/admin/roles/<role_id>/authorities
Authorization: Bearer <your_token>
Content-Type: application/json

{
  "authority_ids": ["authority_id_1", "authority_id_2", "authority_id_3"]
}
```

#### 获取角色权限
```bash
GET http://localhost:8080/api/v1/admin/roles/<role_id>/authorities
Authorization: Bearer <your_token>
```

## 预期结果

所有请求应该返回以下格式的成功响应：

```json
{
  "code": 0,
  "message": "成功",
  "data": { ... }
}
```

失败响应：

```json
{
  "code": 403,
  "message": "无权限访问该资源",
  "data": null
}
```

## 注意事项

1. 所有请求都需要在 Header 中包含有效的 JWT token
2. 确保用户有足够的权限执行操作
3. UUID 格式必须正确
4. 删除操作不可恢复，请谨慎测试

## 故障排除

如果遇到问题：

1. 检查数据库连接是否正常
2. 检查 Casbin 是否初始化成功
3. 查看服务器日志
4. 验证 token 是否有效
5. 确认用户权限配置正确
