# 服务器部署指南

## 环境信息
- 服务器地址: 119.91.157.252
- 面板地址: https://119.91.157.252:13449
- 部署目录: /www/wwwroot/paper-service

---

## 部署步骤

### 方式一：通过 BT 面板操作

1. 登录 BT 面板 (https://119.91.157.252:13449)

2. 点击左侧「文件」菜单，进入 `/www/wwwroot/` 目录

3. 点击「上传」按钮，上传 `deploy-package.zip` 文件

4. 上传完成后，选中 zip 文件，点击「解压」

5. 解压后进入 `paper-service` 目录，确认包含以下文件：
   - Dockerfile
   - docker-compose.yml
   - .env
   - deploy.sh
   - quick-deploy.sh

6. 点击「终端」打开命令行终端

7. 执行部署脚本：
   ```bash
   cd /www/wwwroot/paper-service
   chmod +x deploy.sh
   ./deploy.sh
   ```

### 方式二：通过 SSH 连接（推荐）

1. 使用 SSH 工具连接服务器：
   ```bash
   ssh root@119.91.157.252
   ```

2. 创建项目目录并进入：
   ```bash
   mkdir -p /www/wwwroot/paper-service
   cd /www/wwwroot/paper-service
   ```

3. 下载或上传代码到该目录

4. 设置脚本执行权限：
   ```bash
   chmod +x deploy.sh quick-deploy.sh
   ```

5. 编辑 `.env` 文件，配置数据库和密钥：
   ```bash
   nano .env
   ```
   **重要配置项**：
   - `DATABASE_HOST`: 数据库地址
   - `DATABASE_PASSWORD`: 数据库密码
   - `JWT_SECRET`: JWT密钥（请修改为强密码）

6. 执行部署：
   ```bash
   ./deploy.sh
   ```

---

## 部署后配置

### 1. 配置 Nginx 反向代理（可选）

如果需要通过域名访问，创建 Nginx 配置：

```bash
nano /etc/nginx/sites-available/paper-service
```

写入以下内容：

```nginx
server {
    listen 80;
    server_name your-domain.com;

    client_max_body_size 50M;

    location / {
        proxy_pass http://127.0.0.1:8002;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

启用配置：
```bash
ln -s /etc/nginx/sites-available/paper-service /etc/nginx/sites-enabled/
nginx -t
systemctl reload nginx
```

### 2. 配置 SSL 证书（可选）

使用 Let's Encrypt 免费证书：
```bash
certbot --nginx -d your-domain.com
```

### 3. 配置微信/支付宝登录回调地址

在微信开放平台配置授权回调域名为您的域名：
- 微信登录回调: `https://your-domain.com/api/auth/wechat/callback`
- 支付宝登录回调: `https://your-domain.com/api/auth/alipay/callback`

---

## 常用运维命令

```bash
# 进入项目目录
cd /www/wwwroot/paper-service

# 查看服务状态
docker-compose ps

# 查看日志
docker-compose logs -f

# 重启服务
docker-compose restart

# 停止服务
docker-compose down

# 重新构建并启动
docker-compose up -d --build

# 重新执行部署
./deploy.sh
```

---

## 健康检查

部署完成后访问：
- 本地: http://119.91.157.252:8002/health
- 配置 Nginx 后: https://your-domain.com/health

---

## 常见问题排查

### 1. 服务启动失败
```bash
# 查看详细日志
docker-compose logs paper-service
```

### 2. 数据库连接失败
检查 `.env` 中的数据库配置是否正确：
- `DATABASE_HOST`: 数据库地址（如果是 Docker 外部数据库，使用宿主机 IP）
- `DATABASE_PORT`: 数据库端口（默认 5432）
- `DATABASE_PASSWORD`: 数据库密码

### 3. 端口被占用
```bash
# 查看 8002 端口占用
netstat -tlnp | grep 8002

# 或修改 docker-compose.yml 中的端口映射
```