#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "请使用 sudo 运行：sudo ./install.sh" >&2
  exit 1
fi

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_BINARY="${SCRIPT_DIR}/pal-agent"
if [[ ! -f "${DEFAULT_BINARY}" ]]; then
  DEFAULT_BINARY="${SCRIPT_DIR}/../../build/bin/pal-agent-linux-amd64"
fi
SOURCE_BINARY="${1:-${DEFAULT_BINARY}}"

if [[ ! -f "${SOURCE_BINARY}" ]]; then
  echo "找不到 Linux Agent：${SOURCE_BINARY}" >&2
  exit 1
fi

if command -v apt-get >/dev/null 2>&1; then
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl tar gzip lib32gcc-s1
elif command -v dnf >/dev/null 2>&1; then
  dnf install -y ca-certificates curl tar gzip glibc.i686 libstdc++.i686
elif command -v yum >/dev/null 2>&1; then
  yum install -y ca-certificates curl tar gzip glibc.i686 libstdc++.i686
else
  echo "未识别软件包管理器，请手动安装 ca-certificates、curl、tar、gzip 和 SteamCMD 所需 32 位运行库。" >&2
fi

if ! id palserver >/dev/null 2>&1; then
  useradd --system --create-home --home-dir /var/lib/palserver --shell /usr/sbin/nologin palserver
fi

install -d -o palserver -g palserver -m 0750 /var/lib/palserver /var/lib/palserver-launcher /var/lib/palserver-launcher/bin /var/lib/palserver-launcher/servers
TARGET_BINARY=/var/lib/palserver-launcher/bin/pal-agent
TARGET_SERVICE=/etc/systemd/system/palserver-agent.service
NEW_BINARY="${TARGET_BINARY}.install-new"
OLD_BINARY="${TARGET_BINARY}.install-previous"
NEW_SERVICE="${TARGET_SERVICE}.install-new"
OLD_SERVICE="${TARGET_SERVICE}.install-previous"
HAD_BINARY=0
HAD_SERVICE=0
SWAPPED=0

rollback_install() {
  set +e
  systemctl stop palserver-agent.service >/dev/null 2>&1
  if [[ "${HAD_BINARY}" -eq 1 && -f "${OLD_BINARY}" ]]; then
    rm -f "${TARGET_BINARY}.install-failed"
    [[ -f "${TARGET_BINARY}" ]] && mv "${TARGET_BINARY}" "${TARGET_BINARY}.install-failed"
    mv "${OLD_BINARY}" "${TARGET_BINARY}"
  else
    rm -f "${TARGET_BINARY}"
  fi
  if [[ "${HAD_SERVICE}" -eq 1 && -f "${OLD_SERVICE}" ]]; then
    mv "${OLD_SERVICE}" "${TARGET_SERVICE}"
  else
    rm -f "${TARGET_SERVICE}"
  fi
  systemctl daemon-reload
  if [[ "${HAD_BINARY}" -eq 1 && "${HAD_SERVICE}" -eq 1 ]]; then
    systemctl restart palserver-agent.service >/dev/null 2>&1
  fi
}

cleanup_install() {
  code=$?
  trap - EXIT
  rm -f "${NEW_BINARY}" "${NEW_SERVICE}"
  if [[ "${code}" -ne 0 && "${SWAPPED}" -eq 1 ]]; then
    echo "新版本启动失败，正在恢复上一版本。" >&2
    rollback_install
  fi
  exit "${code}"
}
trap cleanup_install EXIT

rm -f "${NEW_BINARY}" "${NEW_SERVICE}" "${OLD_BINARY}" "${OLD_SERVICE}"
install -o palserver -g palserver -m 0755 "${SOURCE_BINARY}" "${NEW_BINARY}"
install -m 0644 "${SCRIPT_DIR}/palserver-agent.service" "${NEW_SERVICE}"

env PALSERVER_LAUNCHER_HOME=/var/lib/palserver-launcher HOME=/var/lib/palserver "${NEW_BINARY}" --version >/dev/null
if command -v runuser >/dev/null 2>&1; then
  runuser -u palserver -- env \
    PALSERVER_LAUNCHER_HOME=/var/lib/palserver-launcher \
    PALSERVER_ALLOWED_SERVER_ROOTS=/var/lib/palserver-launcher/servers \
    HOME=/var/lib/palserver \
    "${NEW_BINARY}" --self-test --auth-file /var/lib/palserver-launcher/admin-auth.json >/dev/null
else
  su -s /bin/sh palserver -c "env PALSERVER_LAUNCHER_HOME=/var/lib/palserver-launcher PALSERVER_ALLOWED_SERVER_ROOTS=/var/lib/palserver-launcher/servers HOME=/var/lib/palserver ${NEW_BINARY} --self-test --auth-file /var/lib/palserver-launcher/admin-auth.json" >/dev/null
fi

if [[ -f "${TARGET_BINARY}" ]]; then
  mv "${TARGET_BINARY}" "${OLD_BINARY}"
  HAD_BINARY=1
fi
mv "${NEW_BINARY}" "${TARGET_BINARY}"
SWAPPED=1

if [[ -f "${TARGET_SERVICE}" ]]; then
  mv "${TARGET_SERVICE}" "${OLD_SERVICE}"
  HAD_SERVICE=1
fi
mv "${NEW_SERVICE}" "${TARGET_SERVICE}"

systemctl daemon-reload
systemctl enable palserver-agent.service
systemctl restart palserver-agent.service

HEALTHY=0
for _ in {1..40}; do
  if curl -fsS --max-time 2 http://127.0.0.1:8210/api/v1/health >/dev/null 2>&1; then
    HEALTHY=1
    break
  fi
  if ! systemctl is-active --quiet palserver-agent.service; then
    break
  fi
  sleep 0.25
done

if [[ "${HEALTHY}" -ne 1 ]]; then
  echo "Palserver Linux Agent 未通过启动健康检查。" >&2
  journalctl -u palserver-agent.service -n 30 --no-pager >&2 || true
  exit 1
fi

rm -f "${OLD_BINARY}" "${OLD_SERVICE}" "${TARGET_BINARY}.install-failed"
# 0.1.5 and earlier created a reusable bearer token. Password authentication
# no longer reads it; remove it only after the new service passes health checks
# so an automatic rollback can still start the previous version.
rm -f /var/lib/palserver-launcher/agent-token
SWAPPED=0

echo
echo "Palserver Linux Agent 已安装。"
echo "本机地址：http://127.0.0.1:8210"
echo "推荐使用 SSH 隧道访问：ssh -L 8210:127.0.0.1:8210 user@server"
echo "然后在本地浏览器打开：http://127.0.0.1:8210"
if [[ -s /var/lib/palserver-launcher/admin-auth.json ]]; then
  echo "管理密码已保留，请使用原密码登录。"
else
  echo "首次打开网页后，请按提示创建管理密码。"
fi
