import assert from 'node:assert/strict';
import test from 'node:test';

import { MAP_CONFIGS, projectPlayerLocation } from '../src/mapConfig.ts';

test('旧地图使用原有坐标投影', () => {
  assert.deepEqual(projectPlayerLocation('legacy', -123467.16, 157664.56), {
    left: 50,
    top: 50,
  });
});

test('1.0 世界树地图使用独立边界投影', () => {
  assert.deepEqual(projectPlayerLocation('v1', 689148.5, -818197), {
    left: 0,
    top: 0,
  });
  assert.deepEqual(projectPlayerLocation('v1', 347351.5, -476400), {
    left: 100,
    top: 100,
  });
});

test('所选地图范围外的坐标不显示', () => {
  assert.equal(projectPlayerLocation('legacy', -123467.16, -73815.45), null);
  assert.equal(projectPlayerLocation('v1', 347351.5, -476399), null);
});

test('地图配置提供独立底图并默认包含 1.0 世界树地图', () => {
  assert.equal(MAP_CONFIGS.legacy.image, '/map/palworld-world-map.webp');
  assert.equal(MAP_CONFIGS.v1.image, '/map/palworld-world-tree-map.png');
  assert.equal(MAP_CONFIGS.v1.label, '1.0 世界树');
});
