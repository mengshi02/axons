# 部署指南

本指南涵盖 Axons 的各种部署选项。

## 目录

- [先决条件](#先决条件)
- [二进制部署](#二进制部署)
- [Docker 部署](#docker-部署)
- [Kubernetes 部署](#kubernetes-部署)
- [Systemd 服务](#systemd-服务)
- [生产环境考虑](#生产环境考虑)

## 先决条件

### 系统要求

- **操作系统**：Linux、macOS 或 Windows
- **架构**：AMD64 或 ARM64
- **内存**：最低 512MB RAM（推荐：2GB+）
- **磁盘**：二进制文件最低 100MB（取决于数据库大小）

### 软件要求

- **Go**：1.25.0+（用于源码构建）
- **Node.js**：22+（用于前端构建）

## 二进制部署

### 下载预构建二进制文件

从 [GitHub Releases](https://github.com/mengshi02/axons/releases) 页面下载最新版本。

```bash
# Linux AMD64
wget https://github.com/mengshi02/axons/releases/latest/download/axons-linux-amd64.tar.gz
tar -xzf axons-linux-amd64.tar.gz
sudo mv axons-linux-amd64 /usr/local/bin/axons

# macOS ARM64（Apple Silicon）
wget https://github.com/mengshi02/axons/releases/latest/download/axons-darwin-arm64.tar.gz
tar -xzf axons-darwin-arm64.tar.gz
sudo mv axons-darwin-arm64 /usr/local/bin/axons

# Windows AMD64
# 下载 axons-windows-amd64.zip 并解压
```

### 源码构建

```bash
# 克隆仓库
git clone https://github.com/mengshi02/axons.git
cd axons

# 构建
make build

# 安装到 GOPATH/bin
make install
```

### 运行二进制文件

```bash
# 启动守护进程
axons daemon start --tcp :8080

# 检查状态
axons daemon ps

# 停止守护进程
axons daemon stop
```

## Docker 部署

### 使用 Docker Compose（推荐）

创建 `docker-compose.yml` 文件：

```yaml
version: '3.8'

services:
  axons:
    image: ghcr.io/mengshi02/axons:latest
    ports:
      - "8080:8080"
    volumes:
      - axons-data:/data
      - ./config:/config
    environment:
      - AXONS_DATABASE_PATH=/data/axons.db
      - AXONS_LOGGING_LEVEL=info
    restart: unless-stopped

volumes:
  axons-data:
```

启动服务：

```bash
docker-compose up -d
```

### 使用 Docker CLI

```bash
# 构建镜像
docker build -t axons:latest .

# 运行容器
docker run -d \
  --name axons \
  -p 8080:8080 \
  -v axons-data:/data \
  -e AXONS_DATABASE_PATH=/data/axons.db \
  axons:latest
```

### Dockerfile

如果您想构建自己的 Docker 镜像：

```dockerfile
# 构建阶段
FROM golang:1.25-alpine AS builder

# 注意：需要 Go 1.25。go.mod 指定 go 1.25.0。

RUN apk add --no-cache nodejs npm make git

WORKDIR /app
COPY . .

RUN make build

# 最终阶段
FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /app/bin/axons /usr/local/bin/axons

EXPOSE 8080

CMD ["axons", "daemon", "start", "--tcp", ":8080"]
```

## Kubernetes 部署

### 部署清单

创建 `kubernetes-deployment.yaml`：

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: axons
  labels:
    app: axons
spec:
  replicas: 1
  selector:
    matchLabels:
      app: axons
  template:
    metadata:
      labels:
        app: axons
    spec:
      containers:
      - name: axons
        image: ghcr.io/mengshi02/axons:latest
        ports:
        - containerPort: 8080
        volumeMounts:
        - name: data
          mountPath: /data
        env:
        - name: AXONS_DATABASE_PATH
          value: /data/axons.db
        - name: AXONS_LOGGING_LEVEL
          value: info
        resources:
          requests:
            memory: "512Mi"
            cpu: "250m"
          limits:
            memory: "2Gi"
            cpu: "1000m"
      volumes:
      - name: data
        persistentVolumeClaim:
          claimName: axons-pvc
---
apiVersion: v1
kind: Service
metadata:
  name: axons
spec:
  selector:
    app: axons
  ports:
  - port: 80
    targetPort: 8080
  type: ClusterIP
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: axons-pvc
spec:
  accessModes:
  - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
```

应用清单：

```bash
kubectl apply -f kubernetes-deployment.yaml
```

### Helm Chart（未来）

将提供 Helm chart 用于更简单的 Kubernetes 部署。

## Systemd 服务

为自动启动创建 systemd 服务文件：

### 服务文件

创建 `/etc/systemd/system/axons.service`：

```ini
[Unit]
Description=Axons 代码知识图谱
After=network.target

[Service]
Type=simple
User=axons
Group=axons
ExecStart=/usr/local/bin/axons daemon start --tcp :8080
ExecStop=/usr/local/bin/axons daemon stop
Restart=on-failure
RestartSec=5

# 环境
Environment=AXONS_DATABASE_PATH=/var/lib/axons/axons.db
Environment=AXONS_LOGGING_LEVEL=info

# 安全
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

### 设置

```bash
# 创建用户
sudo useradd -r -s /bin/false axons

# 创建数据目录
sudo mkdir -p /var/lib/axons
sudo chown axons:axons /var/lib/axons

# 重新加载 systemd
sudo systemctl daemon-reload

# 启用并启动
sudo systemctl enable axons
sudo systemctl start axons

# 检查状态
sudo systemctl status axons
```

### 日志管理

使用 journalctl 查看日志：

```bash
# 查看所有日志
sudo journalctl -u axons

# 跟踪日志
sudo journalctl -u axons -f

# 查看最后 100 行
sudo journalctl -u axons -n 100
```

## 生产环境考虑

### 高可用性

对于生产部署，考虑：

1. **负载均衡器**：在 Axons 前使用 nginx 或 HAProxy
2. **多实例**：在负载均衡器后运行多个实例
3. **数据库**：使用共享存储或迁移到分布式数据库

### 反向代理（nginx）

```nginx
server {
    listen 80;
    server_name axons.chat;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### SSL/TLS

使用 Let's Encrypt 和 certbot：

```bash
# 安装 certbot
sudo apt install certbot python3-certbot-nginx

# 获取证书
sudo certbot --nginx -d axons.chat
```

### 监控

1. **健康检查端点**：`GET /health` 返回服务状态
2. **就绪检查端点**：`GET /ready` 返回就绪状态
3. **守护进程状态**：`GET /api/v1/status` 返回守护进程状态信息
4. **日志记录**：配置结构化日志用于日志聚合

### 备份

定期备份数据库：

```bash
#!/bin/bash
# backup.sh

BACKUP_DIR="/var/backups/axons"
DATE=$(date +%Y%m%d_%H%M%S)

mkdir -p $BACKUP_DIR
cp /var/lib/axons/axons.db "$BACKUP_DIR/axons_$DATE.db"

# 只保留最近 7 天
find $BACKUP_DIR -name "axons_*.db" -mtime +7 -delete
```

添加到 crontab：

```bash
# 每天凌晨 2 点备份
0 2 * * * /path/to/backup.sh
```

### 安全检查清单

- [ ] 以非 root 用户运行
- [ ] 生产环境使用 HTTPS
- [ ] 使用防火墙限制网络访问
- [ ] 保持二进制文件更新
- [ ] 监控日志中的可疑活动
- [ ] 设置适当的文件权限

## 故障排除

### 常见问题

1. **端口已被占用**
   ```bash
   # 查找使用端口的进程
   lsof -i :8080
   # 终止进程或更改端口
   ```

2. **权限被拒绝**
   ```bash
   # 检查文件权限
   ls -la /var/lib/axons/
   # 修复权限
   chown -R axons:axons /var/lib/axons/
   ```

3. **数据库锁定**
   ```bash
   # 停止服务并移除锁
   systemctl stop axons
   rm /var/lib/axons/axons.db-wal
   systemctl start axons
   ```

### 获取帮助

- **网站**：[axons.chat](https://axons.chat)
- **邮箱**：[support@axons.chat](mailto:support@axons.chat)
- 查看日志：`journalctl -u axons -n 100`
- GitHub Issues：https://github.com/mengshi02/axons/issues
- 文档：[docs/](./)