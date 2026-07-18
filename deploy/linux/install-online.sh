#!/usr/bin/env bash
set -Eeuo pipefail

# One-command installer for a clean Linux host. The script downloads the
# signed-by-checksum release bundle, then delegates installation and rollback
# to the same installer shipped inside the offline bundle.
REPOSITORY="${PALSERVER_REPOSITORY:-zhumengling/Palserver-Launcher}"
VERSION="${PALSERVER_VERSION:-latest}"
ASSET="palserver-agent-linux-amd64.tar.gz"
CHECKSUM_ASSET="${ASSET}.sha256"
DOWNLOAD_ROOT="https://github.com/${REPOSITORY}/releases/download"

if [[ "${EUID}" -ne 0 ]]; then
  echo "请使用 sudo 运行：curl -fsSL https://raw.githubusercontent.com/${REPOSITORY}/main/deploy/linux/install-online.sh | sudo bash" >&2
  exit 1
fi

if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y ca-certificates curl
  elif command -v yum >/dev/null 2>&1; then
    yum install -y ca-certificates curl
  else
    echo "缺少 curl 或 wget，且无法识别软件包管理器。" >&2
    exit 1
  fi
fi

for command in tar gzip sha256sum; do
  if ! command -v "${command}" >/dev/null 2>&1; then
    echo "缺少 ${command}。请先安装 tar、gzip 和 sha256sum 后重试。" >&2
    exit 1
  fi
done

download() {
  local url="$1"
  local destination="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fL --retry 3 --retry-delay 2 --progress-bar "${url}" -o "${destination}"
  else
    wget --tries=3 --timeout=30 --show-progress -O "${destination}" "${url}"
  fi
}

temporary="$(mktemp -d /tmp/palserver-agent-online.XXXXXX)"
cleanup() {
  rm -rf "${temporary}"
}
trap cleanup EXIT

if [[ "${PALSERVER_RELEASE_URL:-}" != "" ]]; then
  release_url="${PALSERVER_RELEASE_URL}"
  checksum_url="${PALSERVER_CHECKSUM_URL:-${release_url}.sha256}"
  release_label="自定义 Release"
elif [[ "${VERSION}" == "latest" ]]; then
  release_url="https://github.com/${REPOSITORY}/releases/latest/download/${ASSET}"
  checksum_url="https://github.com/${REPOSITORY}/releases/latest/download/${CHECKSUM_ASSET}"
  release_label="最新版本"
else
  release_tag="${VERSION}"
  if [[ "${release_tag}" != v* ]]; then
    release_tag="v${release_tag}"
  fi
  release_url="${DOWNLOAD_ROOT}/${release_tag}/${ASSET}"
  checksum_url="${DOWNLOAD_ROOT}/${release_tag}/${CHECKSUM_ASSET}"
  release_label="${release_tag}"
fi

echo "正在下载 Palserver Linux Agent ${release_label}"
download "${release_url}" "${temporary}/${ASSET}"
download "${checksum_url}" "${temporary}/${CHECKSUM_ASSET}"

checksum_line="$(grep -E "[[:space:]]${ASSET}$" "${temporary}/${CHECKSUM_ASSET}" | head -n 1 || true)"
if [[ -z "${checksum_line}" ]]; then
  echo "Release 没有 ${ASSET} 的有效 SHA-256 校验值，已停止安装。" >&2
  exit 1
fi
printf '%s\n' "${checksum_line}" | (cd "${temporary}" && sha256sum -c -)

mkdir -p "${temporary}/bundle"
tar -xzf "${temporary}/${ASSET}" -C "${temporary}/bundle"
if [[ ! -f "${temporary}/bundle/install.sh" || ! -f "${temporary}/bundle/pal-agent" ]]; then
  echo "Release 安装包内容不完整，已停止安装。" >&2
  exit 1
fi

chmod 0755 "${temporary}/bundle/install.sh"
bash "${temporary}/bundle/install.sh"

echo
echo "一键安装完成。请在浏览器打开 Agent 地址，首次访问时创建管理密码。"
