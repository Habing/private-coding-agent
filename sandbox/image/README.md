# pca/sandbox:base

沙箱默认基础镜像。debian:12-slim + 常用编程工具链。

## 构建

```bash
docker build -t pca/sandbox:base ./sandbox/image
```

预计镜像大小 ~1.2 GB（首次构建 5-10 分钟）。

## 工具清单

- 通用：git curl wget jq tree ripgrep make less vim-tiny
- Go 工具链（debian 仓库版本）
- Node.js + npm
- Python 3 + pip
- build-essential（gcc/g++/make）

## 用户

`sandbox` (uid=10001, gid=10001) — 非 root，WORKDIR `/workspace`。

## 升级

更新 Dockerfile 后重新 build。不打 latest tag；推荐月度 tag 如 `pca/sandbox:base-2026.05`。
