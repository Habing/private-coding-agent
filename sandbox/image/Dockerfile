# pca/sandbox:base — 沙箱默认基础镜像
# 预装常用 CLI + Go/Node/Python 工具链，让会话即起即用
FROM debian:12-slim

ARG DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl wget git \
      jq tree ripgrep make \
      golang nodejs npm python3 python3-pip \
      build-essential \
      vim-tiny less \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# 沙箱用户:固定 uid 10001 避免与主机用户冲突
RUN groupadd -g 10001 sandbox \
    && useradd -m -u 10001 -g 10001 -s /bin/bash sandbox

USER sandbox
WORKDIR /workspace

# 默认睡到天荒地老,真正的工作通过 exec 注入
CMD ["sleep", "infinity"]
