# GitHub Actions 持续部署说明

这套 CI/CD 按当前线上真实部署方式设计：后端不是 Docker，而是 systemd 服务。

- 线上目录：`/opt/paper`
- 线上二进制：`/opt/paper/paper-server`
- 服务名：`paper.service`
- 监听端口：`8002`
- 健康检查：`http://127.0.0.1:8002/health`

## 1. GitHub 仓库准备

如果当前本地远程仓库还是 Gitee，先改成 GitHub。

在项目目录执行：

```bash
cd C:\Users\user\.config\superpowers\worktrees\paper\docx-closed-loop-task1
git remote -v
```

如果还是 `https://gitee.com/yi-an-li/paper`，改成你的 GitHub 仓库地址：

```bash
git remote set-url origin https://github.com/YOUR_GITHUB_USER/YOUR_REPO.git
git remote -v
```

如果 GitHub 仓库还没创建，先在 GitHub 新建一个空仓库，再执行上面的命令。

## 2. GitHub Secrets

进入 GitHub 仓库：

`Settings` -> `Secrets and variables` -> `Actions` -> `Secrets` -> `New repository secret`

新增这些 Secrets：

| 名称 | 值 |
| --- | --- |
| `PROD_HOST` | `119.91.157.252` |
| `PROD_PORT` | `22` |
| `PROD_USER` | `root` |
| `PROD_SSH_KEY` | 能登录服务器的 SSH 私钥内容 |

注意：`PROD_SSH_KEY` 是私钥文本，不要提交到仓库。

## 3. GitHub Variables

进入：

`Settings` -> `Secrets and variables` -> `Actions` -> `Variables`

可以新增这些 Variables；不新增也可以，工作流已有默认值。

| 名称 | 默认值 |
| --- | --- |
| `PROD_DEPLOY_DIR` | `/opt/paper` |
| `PROD_SERVICE_NAME` | `paper.service` |
| `PROD_SERVER_PORT` | `8002` |
| `PROD_HEALTH_URL` | `http://127.0.0.1:8002/health` |

## 4. 提交并推送

```bash
git add .github/workflows/deploy-backend.yml scripts/deploy_backend_systemd.sh docs/github-actions-cicd-systemd.md .gitignore
git commit -m "ci: add github actions backend deployment"
git push origin main
```

如果你的生产分支是 `master`：

```bash
git push origin master
```

工作流默认监听 `main` 和 `master`。

## 5. 工作流会自动做什么

每次推送到 `main` 或 `master`：

1. 拉取代码
2. 安装 Go 1.24
3. 执行测试：
   ```bash
   go test ./internal/middleware ./internal/handler ./internal/service ./cmd/server
   ```
4. 构建 Linux 二进制：
   ```bash
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o output/paper-server ./cmd/server
   ```
5. 上传到服务器 `/tmp/paper-server.new`
6. 在服务器执行 `scripts/deploy_backend_systemd.sh`
7. 自动备份旧版本：
   ```bash
   /opt/paper/paper-server.backup-YYYYmmdd-HHMMSS
   ```
8. 写入 systemd 覆盖配置，固定 `SERVER_PORT=8002`
9. 替换 `/opt/paper/paper-server`
10. 重启 `paper.service`
11. 健康检查失败时自动回滚旧版本

## 6. 手动运行部署

GitHub 仓库页面：

`Actions` -> `Deploy backend` -> `Run workflow`

选择 `main` 或 `master` 后运行。

## 7. 线上验证

在宝塔终端或 SSH 里执行：

```bash
cd /opt/paper
systemctl status paper.service --no-pager
ss -lntp | grep 8002
curl -i http://127.0.0.1:8002/health
journalctl -u paper.service -n 80 --no-pager
```

如果 `paper.service` 是 `active (running)`，`8002` 有监听，`/health` 返回 200，部署成功。

## 8. 紧急回滚

```bash
cd /opt/paper
ls -lh paper-server.backup-*
systemctl stop paper.service
cp paper-server.backup-YYYYmmdd-HHMMSS paper-server
chmod +x paper-server
chown root:root paper-server
systemctl start paper.service
systemctl status paper.service --no-pager
curl -i http://127.0.0.1:8002/health
```

把 `paper-server.backup-YYYYmmdd-HHMMSS` 换成要恢复的备份文件名。
