# Deployment Guide

This guide covers various deployment options for Axons.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Binary Deployment](#binary-deployment)
- [Docker Deployment](#docker-deployment)
- [Kubernetes Deployment](#kubernetes-deployment)
- [Systemd Service](#systemd-service)
- [Production Considerations](#production-considerations)

## Prerequisites

### System Requirements

- **OS**: Linux, macOS, or Windows
- **Architecture**: AMD64 or ARM64
- **Memory**: Minimum 512MB RAM (recommended: 2GB+)
- **Disk**: Minimum 100MB for binary (depends on database size)

### Software Requirements

- **Go**: 1.25.0+ (for building from source)
- **Node.js**: 22+ (for frontend build)

## Binary Deployment

### Download Pre-built Binary

Download the latest release from the [GitHub Releases](https://github.com/mengshi02/axons/releases) page.

```bash
# Linux AMD64
wget https://github.com/mengshi02/axons/releases/latest/download/axons-linux-amd64.tar.gz
tar -xzf axons-linux-amd64.tar.gz
sudo mv axons-linux-amd64 /usr/local/bin/axons

# macOS ARM64 (Apple Silicon)
wget https://github.com/mengshi02/axons/releases/latest/download/axons-darwin-arm64.tar.gz
tar -xzf axons-darwin-arm64.tar.gz
sudo mv axons-darwin-arm64 /usr/local/bin/axons

# Windows AMD64
# Download axons-windows-amd64.zip and extract
```

### Build from Source

```bash
# Clone the repository
git clone https://github.com/mengshi02/axons.git
cd axons

# Build
make build

# Install to GOPATH/bin
make install
```

### Run the Binary

```bash
# Start the daemon
axons daemon start --tcp :8080

# Check status
axons daemon ps

# Stop the daemon
axons daemon stop
```

## Docker Deployment

### Using Docker Compose (Recommended)

Create a `docker-compose.yml` file:

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

Start the service:

```bash
docker-compose up -d
```

### Using Docker CLI

```bash
# Build the image
docker build -t axons:latest .

# Run the container
docker run -d \
  --name axons \
  -p 8080:8080 \
  -v axons-data:/data \
  -e AXONS_DATABASE_PATH=/data/axons.db \
  axons:latest
```

### Dockerfile

If you want to build your own Docker image:

```dockerfile
# Build stage
FROM golang:1.25-alpine AS builder

# Note: Go 1.25 is required. The go.mod specifies go 1.25.0.

RUN apk add --no-cache nodejs npm make git

WORKDIR /app
COPY . .

RUN make build

# Final stage
FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /app/bin/axons /usr/local/bin/axons

EXPOSE 8080

CMD ["axons", "daemon", "start", "--tcp", ":8080"]
```

## Kubernetes Deployment

### Deployment Manifest

Create `kubernetes-deployment.yaml`:

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

Apply the manifest:

```bash
kubectl apply -f kubernetes-deployment.yaml
```

### Helm Chart (Future)

A Helm chart will be available for easier Kubernetes deployment.

## Systemd Service

Create a systemd service file for automatic startup:

### Service File

Create `/etc/systemd/system/axons.service`:

```ini
[Unit]
Description=Axons Code Knowledge Graph
After=network.target

[Service]
Type=simple
User=axons
Group=axons
ExecStart=/usr/local/bin/axons daemon start --tcp :8080
ExecStop=/usr/local/bin/axons daemon stop
Restart=on-failure
RestartSec=5

# Environment
Environment=AXONS_DATABASE_PATH=/var/lib/axons/axons.db
Environment=AXONS_LOGGING_LEVEL=info

# Security
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

### Setup

```bash
# Create user
sudo useradd -r -s /bin/false axons

# Create data directory
sudo mkdir -p /var/lib/axons
sudo chown axons:axons /var/lib/axons

# Reload systemd
sudo systemctl daemon-reload

# Enable and start
sudo systemctl enable axons
sudo systemctl start axons

# Check status
sudo systemctl status axons
```

### Log Management

View logs with journalctl:

```bash
# View all logs
sudo journalctl -u axons

# Follow logs
sudo journalctl -u axons -f

# View last 100 lines
sudo journalctl -u axons -n 100
```

## Production Considerations

### High Availability

For production deployments, consider:

1. **Load Balancer**: Use nginx or HAProxy in front of Axons
2. **Multiple Instances**: Run multiple instances behind a load balancer
3. **Database**: Use shared storage or migrate to a distributed database

### Reverse Proxy (nginx)

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

Use Let's Encrypt with certbot:

```bash
# Install certbot
sudo apt install certbot python3-certbot-nginx

# Get certificate
sudo certbot --nginx -d axons.chat
```

### Monitoring

1. **Health Check Endpoint**: `GET /health` returns service status
2. **Readiness Check Endpoint**: `GET /ready` returns readiness status
3. **Daemon Status**: `GET /api/v1/status` returns daemon status information
4. **Logging**: Configure structured logging for log aggregation

### Backup

Regular backups of the database:

```bash
#!/bin/bash
# backup.sh

BACKUP_DIR="/var/backups/axons"
DATE=$(date +%Y%m%d_%H%M%S)

mkdir -p $BACKUP_DIR
cp /var/lib/axons/axons.db "$BACKUP_DIR/axons_$DATE.db"

# Keep only last 7 days
find $BACKUP_DIR -name "axons_*.db" -mtime +7 -delete
```

Add to crontab:

```bash
# Daily backup at 2 AM
0 2 * * * /path/to/backup.sh
```

### Security Checklist

- [ ] Run as non-root user
- [ ] Use HTTPS in production
- [ ] Restrict network access with firewall
- [ ] Keep the binary updated
- [ ] Monitor logs for suspicious activity
- [ ] Set appropriate file permissions

## Troubleshooting

### Common Issues

1. **Port already in use**
   ```bash
   # Find process using port
   lsof -i :8080
   # Kill process or change port
   ```

2. **Permission denied**
   ```bash
   # Check file permissions
   ls -la /var/lib/axons/
   # Fix permissions
   chown -R axons:axons /var/lib/axons/
   ```

3. **Database locked**
   ```bash
   # Stop service and remove lock
   systemctl stop axons
   rm /var/lib/axons/axons.db-wal
   systemctl start axons
   ```

### Getting Help

- **Website**: [axons.chat](https://www.axons.chat)
- **Email**: [support@axons.chat](mailto:support@axons.chat)
- Check logs: `journalctl -u axons -n 100`
- GitHub Issues: https://github.com/mengshi02/axons/issues
- Documentation: [docs/](./)