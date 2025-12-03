#!/bin/bash
# 配置文件路径（和原 command 完全一致）
REDIS_CONF_PATH="/usr/local/etc/redis/redis.conf"
TARGET_REPLICAOF="replicaof localhost 6379"

# ====================== 第一步：持久化修改 redis.conf（核心）======================
echo "📝 处理配置文件：$REDIS_CONF_PATH"
if grep -q "^replicaof" "$REDIS_CONF_PATH"; then
  # 替换现有 replicaof 配置（兼容 busybox 镜像）
  sed -i'' "s/^replicaof .*/$TARGET_REPLICAOF/" "$REDIS_CONF_PATH"
  echo "✅ 替换为目标配置：$TARGET_REPLICAOF"
else
  # 追加 replicaof 配置（若缺失）
  echo -e "\n$TARGET_REPLICAOF" >> "$REDIS_CONF_PATH"
  echo "✅ 新增目标配置：$TARGET_REPLICAOF"
fi

# ====================== 第二步：启动 Redis（和原 command 完全一致）======================
echo "🚀 启动 Redis 服务器（加载自定义配置）"
# 直接使用原命令格式，去掉 exec 也可以（exec 可选，优化进程管理）
redis-server /usr/local/etc/redis/redis.conf