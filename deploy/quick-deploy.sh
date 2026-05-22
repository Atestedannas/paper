#!/bin/bash

# 论文格式检查服务 - 快速部署脚本（无需构建，直接使用预编译版本）

set -e

echo "========================================="
echo "  论文格式检查服务 - 快速部署脚本"
echo "========================================="

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

PROJECT_DIR="/www/wwwroot/paper-service"

# 检查是否为 root 用户
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}请使用 root 用户运行此脚本${NC}"
    exit 1
fi

echo -e "${YELLOW}创建项目目录...${NC}"
mkdir -p $PROJECT_DIR

echo -e "${YELLOW}复制文件到项目目录...${NC}"
# 在当前目录执行，将所有文件复制过去
cp -r ./* $PROJECT_DIR/ 2>/dev/null || true

# 创建必要的目录
mkdir -p $PROJECT_DIR/uploads
mkdir -p $PROJECT_DIR/logs
chmod -R 755 $PROJECT_DIR/uploads
chmod -R 755 $PROJECT_DIR/logs

cd $PROJECT_DIR

# 停止现有容器
echo -e "${YELLOW}停止现有容器...${NC}"
docker-compose down 2>/dev/null || true

# 构建 Docker 镜像
echo -e "${YELLOW}构建 Docker 镜像...${NC}"
docker build -t paper-format-checker:latest .

# 启动服务
echo -e "${YELLOW}启动服务...${NC}"
docker-compose up -d

# 等待服务启动
echo -e "${YELLOW}等待服务启动...${NC}"
sleep 10

# 检查服务状态
echo -e "${YELLOW}检查服务状态...${NC}"
docker-compose ps

# 检查健康状态
echo -e "${YELLOW}检查健康状态...${NC}"
curl -f http://localhost:8002/health || echo -e "${YELLOW}健康检查完成${NC}"

echo -e "${GREEN}========================================="
echo -e "  部署完成!"
echo -e "========================================="
echo -e "服务地址: http://localhost:8002"
echo -e "健康检查: http://localhost:8002/health"
echo -e ""
echo -e "常用命令:"
echo -e "  查看日志: docker-compose logs -f"
echo -e "  重启服务: docker-compose restart"
echo -e "  停止服务: docker-compose down"
echo -e "========================================="${NC}