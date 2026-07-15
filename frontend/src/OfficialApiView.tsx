import { useCallback, useEffect, useRef, useState } from "react";
import {
  Activity,
  Clock3,
  Database,
  Gauge,
  RefreshCw,
  Save,
  Server,
  Square,
  Users,
} from "lucide-react";
import * as API from "../wailsjs/go/main/App";
import { main } from "../wailsjs/go/models";

type Props = {
  id: string;
  running: boolean;
  restAvailable: boolean;
  run: (
    label: string,
    action: () => Promise<unknown>,
    success?: string,
  ) => Promise<void>;
};
const emptyInfo = () =>
  new main.ServerInfo({
    version: "",
    servername: "",
    description: "",
    worldguid: "",
  });
const emptyMetrics = () =>
  new main.ServerMetrics({
    serverfps: 0,
    currentplayernum: 0,
    serverframetime: 0,
    maxplayernum: 0,
    uptime: 0,
    basecampnum: 0,
    days: 0,
  });
const emptySettings = () =>
  new main.ServerSettings({ values: {}, entries: [] });
const emptyWorld = () =>
  new main.WorldSnapshot({
    Time: "",
    FPS: 0,
    AverageFPS: 0,
    ActorData: [],
    available: false,
    unavailableReason: "",
  });
async function settle<T>(promise: Promise<T>): Promise<PromiseSettledResult<T>> {
  try {
    return { status: "fulfilled", value: await promise };
  } catch (reason) {
    return { status: "rejected", reason };
  }
}
function formatUptime(seconds: number) {
  const value = Math.max(0, Math.floor(seconds || 0));
  const days = Math.floor(value / 86400);
  const hours = Math.floor((value % 86400) / 3600);
  const minutes = Math.floor((value % 3600) / 60);
  return [days ? `${days} 天` : "", `${hours} 小时`, `${minutes} 分`]
    .filter(Boolean)
    .join(" ");
}

export default function OfficialApiView({
  id,
  running,
  restAvailable,
  run,
}: Props) {
  const [info, setInfo] = useState(emptyInfo);
  const [metrics, setMetrics] = useState(emptyMetrics);
  const [settings, setSettings] = useState(emptySettings);
  const [players, setPlayers] = useState<main.Player[]>([]);
  const [world, setWorld] = useState(emptyWorld);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const gameDataAvailable = useRef<boolean | null>(null);
  const [shutdownWait, setShutdownWait] = useState(5);
  const [shutdownMessage, setShutdownMessage] = useState("Server maintenance");
  const refresh = useCallback(async () => {
    if (!running) {
      setError("服务器未运行，官方 REST API 暂不可用。");
      return;
    }
    setLoading(true);
    setError("");
    const [infoResult, metricsResult, settingsResult, playersResult, worldResult] =
      await Promise.all([
        settle(API.GetServerInfo(id)),
        settle(API.GetServerMetrics(id)),
        settle(API.GetServerSettings(id)),
        settle(API.GetPlayers(id)),
        gameDataAvailable.current === false
          ? Promise.resolve(null)
          : settle(API.GetWorldSnapshot(id)),
      ]);
    const results = [
      infoResult,
      metricsResult,
      settingsResult,
      playersResult,
    ];
    if (infoResult.status === "fulfilled") setInfo(infoResult.value);
    if (metricsResult.status === "fulfilled") setMetrics(metricsResult.value);
    if (settingsResult.status === "fulfilled") setSettings(settingsResult.value);
    if (playersResult.status === "fulfilled") setPlayers(playersResult.value);
    if (worldResult?.status === "fulfilled") {
      setWorld(worldResult.value);
      gameDataAvailable.current = worldResult.value.available;
    }
    if (worldResult?.status === "rejected") results.push(worldResult);
    const failures = results.filter(
      (result) => result.status === "rejected",
    ) as PromiseRejectedResult[];
    if (failures.length)
      setError(
        `${failures.length} 个官方接口请求失败：${String(failures[0].reason)}`,
      );
    setLoading(false);
  }, [id, running]);
  useEffect(() => {
    if (!running) gameDataAvailable.current = null;
  }, [running]);
  useEffect(() => {
    void refresh();
    if (!running) return;
    const timer = window.setInterval(() => void refresh(), 5000);
    return () => window.clearInterval(timer);
  }, [refresh, running]);
  const cards = [
    [metrics.currentplayernum, `在线 / ${metrics.maxplayernum}`, Users],
    [metrics.serverfps.toFixed(1), "服务器 FPS", Gauge],
    [`${metrics.serverframetime.toFixed(2)} ms`, "帧时间", Activity],
    [metrics.basecampnum, "据点数量", Database],
    [metrics.days, "世界天数", Clock3],
    [formatUptime(metrics.uptime), "运行时间", Server],
  ] as const;
  const shutdown = () => {
    if (!confirm(`确定在 ${shutdownWait} 秒后关闭服务器？`)) return;
    void run(
      "official-shutdown",
      () => API.ShutdownServer(id, shutdownWait, shutdownMessage),
      "已通过官方 REST API 发送关服指令",
    );
  };
  return (
    <div className="stack official-api">
      <section className="panel">
        <div className="panel-heading">
          <div>
            <h2>官方 REST API</h2>
            <p>Palworld 1.0 结构化接口 · 每 5 秒自动刷新</p>
          </div>
          <div className="toolbar">
            <span
              className={`badge ${restAvailable ? "success" : "danger-badge"}`}
            >
              {restAvailable ? "REST 在线" : "REST 不可用"}
            </span>
            <button
              className="ghost"
              disabled={loading || !running}
              onClick={() => void refresh()}
            >
              <RefreshCw className={loading ? "spin" : ""} size={15} />
              刷新
            </button>
          </div>
        </div>
        {error && <div className="official-api-error">{error}</div>}
        <div className="official-info">
          <div>
            <span>服务器</span>
            <strong>{info.servername || "-"}</strong>
          </div>
          <div>
            <span>版本</span>
            <code>{info.version || "-"}</code>
          </div>
          <div>
            <span>世界 GUID</span>
            <code>{info.worldguid || "-"}</code>
          </div>
          <div>
            <span>描述</span>
            <strong>{info.description || "-"}</strong>
          </div>
        </div>
      </section>
      <div className="metrics-grid">
        {cards.map(([value, label, Icon]) => (
          <div className="metric" key={label}>
            <Icon size={18} />
            <div>
              <strong>{value}</strong>
              <span>{label}</span>
            </div>
          </div>
        ))}
      </div>
      <div className="two-columns">
        <section className="panel">
          <div className="panel-heading">
            <div>
              <h2>存档与关服</h2>
              <p>官方 POST 操作</p>
            </div>
          </div>
          <div className="action-list">
            <div className="action-row">
              <div className="action-icon">
                <Save size={17} />
              </div>
              <div>
                <strong>立即保存世界</strong>
                <span>调用 /v1/api/save 写入当前世界状态</span>
              </div>
              <button
                className="primary"
                disabled={!running || !restAvailable}
                onClick={() =>
                  void run(
                    "official-save",
                    () => API.SaveWorld(id),
                    "世界存档已保存",
                  )
                }
              >
                保存
              </button>
            </div>
            <div className="official-shutdown">
              <div className="action-icon">
                <Square size={17} />
              </div>
              <div>
                <strong>计划关服</strong>
                <span>向在线玩家发送消息并延迟关闭</span>
                <div className="official-shutdown-fields">
                  <label>
                    等待秒数
                    <input
                      type="number"
                      min="0"
                      value={shutdownWait}
                      onChange={(event) =>
                        setShutdownWait(Math.max(0, Number(event.target.value)))
                      }
                    />
                  </label>
                  <label>
                    关服消息
                    <input
                      value={shutdownMessage}
                      onChange={(event) =>
                        setShutdownMessage(event.target.value)
                      }
                    />
                  </label>
                </div>
              </div>
              <button
                className="danger"
                disabled={!running || !restAvailable}
                onClick={shutdown}
              >
                关服
              </button>
            </div>
          </div>
        </section>
        <section className="panel">
          <div className="panel-heading">
            <div>
              <h2>世界快照</h2>
              <p>
                {world.unavailableReason || world.Time || "等待游戏数据"}
              </p>
            </div>
            <span className={`badge ${world.available ? "success" : ""}`}>
              {world.available
                ? `Actor ${world.ActorData?.length || 0}`
                : world.unavailableReason
                  ? "服务端未启用"
                  : "等待中"}
            </span>
          </div>
          <dl className="details">
            <div>
              <dt>瞬时 FPS</dt>
              <dd>
                <code>{world.FPS?.toFixed(2) || "0.00"}</code>
              </dd>
            </div>
            <div>
              <dt>平均 FPS</dt>
              <dd>
                <code>{world.AverageFPS?.toFixed(2) || "0.00"}</code>
              </dd>
            </div>
          </dl>
        </section>
      </div>
      <section className="panel">
        <div className="panel-heading">
          <div>
            <h2>在线玩家</h2>
            <p>官方 /players 响应的完整字段</p>
          </div>
          <span className="badge success">{players.length} 人在线</span>
        </div>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>玩家</th>
                <th>账号</th>
                <th>Player ID</th>
                <th>User ID</th>
                <th>IP</th>
                <th>延迟</th>
                <th>等级</th>
                <th>建筑</th>
                <th>坐标</th>
              </tr>
            </thead>
            <tbody>
              {players.map((player) => (
                <tr key={`${player.userId}-${player.playerId}`}>
                  <td>
                    <strong>{player.name}</strong>
                  </td>
                  <td>{player.accountName}</td>
                  <td>
                    <code>{player.playerId}</code>
                  </td>
                  <td>
                    <code>{player.userId}</code>
                  </td>
                  <td>
                    <code>{player.ip}</code>
                  </td>
                  <td>{player.ping.toFixed(0)} ms</td>
                  <td>Lv {player.level}</td>
                  <td>{player.buildingCount}</td>
                  <td>
                    {player.locationX.toFixed(0)}, {player.locationY.toFixed(0)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {!players.length && (
            <div className="compact-empty">当前没有在线玩家</div>
          )}
        </div>
      </section>
      <section className="panel">
        <div className="panel-heading">
          <div>
            <h2>世界 Actor</h2>
            <p>
              {world.unavailableReason ||
                "/game-data 中的玩家、帕鲁、NPC 与建筑联合数据"}
            </p>
          </div>
          <span className="badge">{world.ActorData?.length || 0} 条</span>
        </div>
        <div className="table-wrap official-actor-table">
          <table>
            <thead>
              <tr>
                <th>类型 / 实例</th>
                <th>名称 / 职业</th>
                <th>训练师 / 公会</th>
                <th>等级 / 生命</th>
                <th>动作 / 阶段</th>
                <th>坐标</th>
                <th>状态</th>
              </tr>
            </thead>
            <tbody>
              {world.ActorData?.map((actor, index) => (
                <tr key={actor.InstanceID || `${actor.Type}-${index}`}>
                  <td>
                    <strong>{actor.Type || actor.UnitType || "-"}</strong>
                    <small>{actor.InstanceID || "-"}</small>
                  </td>
                  <td>
                    <strong>{actor.NickName || actor.Class || "-"}</strong>
                    <small>
                      {actor.TrainerClass || actor.userid || actor.ip || "-"}
                    </small>
                  </td>
                  <td>
                    <strong>
                      {actor.TrainerNickName || actor.GuildName || "-"}
                    </strong>
                    <small>
                      {actor.TrainerInstanceID || actor.GuildID || "-"}
                    </small>
                  </td>
                  <td>
                    Lv {actor.level || 0}
                    <small>
                      {actor.HP || 0} / {actor.MaxHP || 0} HP
                    </small>
                  </td>
                  <td>
                    {actor.Action || actor.AI_Action || "-"}
                    <small>{actor.Stage || "-"}</small>
                  </td>
                  <td>
                    {actor.LocationX?.toFixed(0) || 0},{" "}
                    {actor.LocationY?.toFixed(0) || 0},{" "}
                    {actor.LocationZ?.toFixed(0) || 0}
                  </td>
                  <td>
                    <span
                      className={`badge ${actor.IsActive ? "success" : ""}`}
                    >
                      {actor.IsActive ? "活跃" : "非活跃"}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {!world.ActorData?.length && (
            <div className="compact-empty">
              {world.unavailableReason || "当前没有 Actor 数据"}
            </div>
          )}
        </div>
      </section>
      <section className="panel">
        <div className="panel-heading">
          <div>
            <h2>官方服务器设置</h2>
            <p>保留布尔、数字与字符串含义，并规范为可展示文本</p>
          </div>
          <span className="badge">{settings.entries?.length || 0} 项</span>
        </div>
        <div className="table-wrap official-settings-table">
          <table>
            <thead>
              <tr>
                <th>设置项</th>
                <th>当前值</th>
              </tr>
            </thead>
            <tbody>
              {settings.entries?.map((entry) => (
                <tr key={entry.key}>
                  <td>
                    <code>{entry.key}</code>
                  </td>
                  <td>{entry.value}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
