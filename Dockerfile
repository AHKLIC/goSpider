FROM golang:1.25.1-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go env -w GOPROXY=https://goproxy.cn,direct && go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o crawlers-app

FROM alpine:latest
RUN apk add --no-cache tzdata
WORKDIR /app
# 仍然创建目录，但不设置特定所有者
RUN mkdir -p logs app-config hot-data
COPY --from=builder /app/crawlers-app .
# 注意：此处省略了 USER 指令，默认将是 root
CMD ["/app/crawlers-app"]