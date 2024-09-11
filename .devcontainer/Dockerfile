FROM mcr.microsoft.com/devcontainers/go:1-1.22-bookworm

RUN mkdir -p /home/vscode/.config/musli
RUN ln -s /workspaces/musli/config.toml /home/vscode/.config/musli/

RUN apt-get update && export DEBIAN_FRONTEND=noninteractive \
    && apt-get -y install --no-install-recommends \
    gcc-mingw-w64 \
    build-essential