# Build stage
FROM golang:1.25 AS builder

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o wall ./cmd/wall

# Runtime stage
FROM alpine:latest

# [新增] 替换为阿里云镜像源
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# [修改] 安装 ca-certificates 和 tzdata，并设置时区为 Asia/Shanghai
RUN apk --no-cache add ca-certificates tzdata \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

# [新增] 设置 TZ 环境变量
ENV TZ=Asia/Shanghai

# Create a non-root user
RUN adduser -D -s /bin/sh appuser

# Set working directory
WORKDIR /home/appuser/

# Copy the binary from builder stage
COPY --from=builder /app/wall .

# Copy default config from example
COPY --from=builder /app/cmd/wall/example_config.yaml ./config.yaml

# Create downloads directory
RUN mkdir -p downloads

# Change ownership to non-root user
RUN chown -R appuser:appuser /home/appuser/

# Switch to non-root user
USER appuser

# Expose port 8080
EXPOSE 8080

# Run the application
CMD ["./wall"]