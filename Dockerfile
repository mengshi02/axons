# Stage 1: Build frontend
FROM node:22-alpine AS frontend

WORKDIR /build/ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

# Stage 2: Build Go binary
FROM golang:1.25-alpine AS builder

ARG VERSION=dev

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=frontend /build/ui/dist ./ui/dist

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-X main.Version=${VERSION} -s -w" \
    -o /bin/axons ./cmd/axons

# Stage 3: Minimal runtime
FROM scratch

COPY --from=builder /bin/axons /axons

EXPOSE 8080

ENTRYPOINT ["/axons"]
CMD ["daemon", "start", "--tcp", ":8080"]