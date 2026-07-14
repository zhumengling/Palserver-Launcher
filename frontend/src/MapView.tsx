import { useEffect, useState } from 'react';
import { Map } from 'lucide-react';

import * as API from '../wailsjs/go/main/App';
import { main } from '../wailsjs/go/models';
import { DEFAULT_MAP_MODE, MAP_CONFIGS, MapMode, MapPoint, projectPlayerLocation } from './mapConfig';

type PlayerMarker = {
  player: main.Player;
  point: MapPoint;
};

const mapModes: MapMode[] = ['legacy', 'v1'];

export default function MapView({ id }: { id: string }) {
  const [players, setPlayers] = useState<main.Player[]>([]);
  const [mode, setMode] = useState<MapMode>(DEFAULT_MAP_MODE);

  useEffect(() => {
    const load = () => API.GetPlayers(id).then(setPlayers).catch(() => setPlayers([]));
    load();
    const timer = setInterval(load, 3000);
    return () => clearInterval(timer);
  }, [id]);

  const map = MAP_CONFIGS[mode];
  const markers = players.reduce<PlayerMarker[]>((visible, player) => {
    const point = projectPlayerLocation(mode, player.locationX, player.locationY);
    if (point) visible.push({ player, point });
    return visible;
  }, []);

  return (
    <section className="panel map-panel">
      <div className="panel-heading map-heading">
        <div>
          <h2>在线地图</h2>
          <p>Palworld 世界地图 · REST API 每 3 秒刷新</p>
        </div>
        <div className="map-heading-actions">
          <div className="segmented map-switch" role="group" aria-label="地图版本">
            {mapModes.map((item) => (
              <button
                type="button"
                className={mode === item ? 'active' : ''}
                aria-pressed={mode === item}
                key={item}
                onClick={() => setMode(item)}
              >
                {MAP_CONFIGS[item].label}
              </button>
            ))}
          </div>
          <span className="badge success">{players.length} 在线</span>
        </div>
      </div>
      <div className="map-canvas">
        <div className="map-stage">
          <img className="world-map-image" src={map.image} alt={map.alt} draggable={false}/>
          <div className="map-grid"/>
          {markers.map(({ player, point }, index) => (
            <div
              className="player-pin"
              title={`${player.name} (${player.locationX.toFixed(0)}, ${player.locationY.toFixed(0)})`}
              key={player.userId}
              style={{ left: `${point.left}%`, top: `${point.top}%` }}
            >
              <span>{index + 1}</span>
              <label>
                <strong>{player.name}</strong>
                <small>{player.locationX.toFixed(0)}, {player.locationY.toFixed(0)}</small>
              </label>
            </div>
          ))}
          {!markers.length && (
            <div className="map-empty">
              <Map size={28}/>
              <span>{players.length ? '在线玩家不在当前地图范围' : '没有可显示的在线玩家'}</span>
            </div>
          )}
        </div>
      </div>
    </section>
  );
}
