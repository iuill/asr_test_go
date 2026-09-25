FROM golang:1.27.1-bookworm AS go-toolchain

FROM mcr.microsoft.com/devcontainers/typescript-node:26-bookworm

ARG WAILS_VERSION=v2.16.0

COPY --from=go-toolchain /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:/home/node/go/bin:${PATH}"
ENV LANG=ja_JP.UTF-8 \
    LC_ALL=ja_JP.UTF-8 \
    TZ=Asia/Tokyo \
    GOTOOLCHAIN=local

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        build-essential gcc-mingw-w64-x86-64 g++-mingw-w64-x86-64 \
        pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev \
        locales tzdata fonts-noto-cjk ripgrep \
    && sed -i 's/^# *ja_JP.UTF-8 UTF-8/ja_JP.UTF-8 UTF-8/' /etc/locale.gen \
    && locale-gen ja_JP.UTF-8 \
    && ln -snf /usr/share/zoneinfo/Asia/Tokyo /etc/localtime \
    && echo Asia/Tokyo > /etc/timezone \
    && rm -rf /var/lib/apt/lists/*

RUN GOBIN=/usr/local/bin go install github.com/wailsapp/wails/v2/cmd/wails@${WAILS_VERSION} \
    && go clean -modcache -cache \
    && install -d -o node -g node /home/node/go /home/node/.codex /home/node/.config/gh
