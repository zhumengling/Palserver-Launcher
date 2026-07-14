export type MapMode = 'legacy' | 'v1';

export type MapPoint = {
  left: number;
  top: number;
};

type MapConfig = {
  label: string;
  image: string;
  alt: string;
  project: (locationX: number, locationY: number) => MapPoint;
};

export const DEFAULT_MAP_MODE: MapMode = 'v1';

export const MAP_CONFIGS: Record<MapMode, MapConfig> = {
  legacy: {
    label: '旧地图',
    image: '/map/palworld-world-map.webp',
    alt: 'Palworld 旧版世界地图',
    project: (locationX, locationY) => ({
      left: ((locationY - 157664.56) / 462.96 + 500) / 10,
      top: ((locationX + 123467.16) / 462.96 + 500) / 10,
    }),
  },
  v1: {
    label: '1.0 世界树',
    image: '/map/palworld-world-tree-map.png',
    alt: 'Palworld 1.0 世界树地图',
    project: (locationX, locationY) => ({
      left: ((locationY + 818197) / 341797) * 100,
      top: (1 - (locationX - 347351.5) / 341797) * 100,
    }),
  },
};

export function projectPlayerLocation(mode: MapMode, locationX: number, locationY: number): MapPoint | null {
  const point = MAP_CONFIGS[mode].project(locationX, locationY);
  if (!Number.isFinite(point.left) || !Number.isFinite(point.top)) return null;
  if (point.left < 0 || point.left > 100 || point.top < 0 || point.top > 100) return null;
  return point;
}
