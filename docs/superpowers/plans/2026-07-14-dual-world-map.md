# 双版本在线地图实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 为在线地图加入旧地图与 1.0 世界树地图切换，并使用两套独立坐标投影。

**架构：** 将地图配置和投影函数放入纯 TypeScript 模块，将地图 UI 从 `App.tsx` 提取为独立组件。地图组件只消费配置模块的统一接口，并过滤选中地图范围外的玩家标记。

**技术栈：** React 19、TypeScript、Vite、Node.js test runner、Wails 2、Go

---

### 任务 1：坐标投影

**文件：**
- 创建：`frontend/tests/mapConfig.test.ts`
- 创建：`frontend/src/mapConfig.ts`

- [ ] **步骤 1：编写失败的测试**

测试旧地图已知中心点、世界树地图边界点以及越界坐标返回 `null`。

- [ ] **步骤 2：运行测试验证失败**

运行：`node --experimental-strip-types --test frontend/tests/mapConfig.test.ts`

预期：FAIL，原因是 `mapConfig.ts` 尚不存在。

- [ ] **步骤 3：编写最少实现代码**

导出 `MapMode`、`MapPoint`、`MAP_CONFIGS` 和 `projectPlayerLocation()`；投影结果只在两个百分比均处于闭区间 `[0, 100]` 时返回。

- [ ] **步骤 4：运行测试验证通过**

运行：`node --experimental-strip-types --test frontend/tests/mapConfig.test.ts`

预期：全部测试 PASS。

### 任务 2：地图组件与素材

**文件：**
- 创建：`frontend/src/MapView.tsx`
- 创建：`frontend/public/map/palworld-world-tree-map.png`
- 修改：`frontend/public/map/NOTICE.md`
- 修改：`frontend/src/App.tsx`
- 修改：`frontend/src/App.css`

- [ ] **步骤 1：安装 1.0 地图素材并记录来源**

将已验证的 palworld.gg 世界树瓦片拼接图放入公共地图目录，NOTICE 记录页面与瓦片来源。

- [ ] **步骤 2：实现独立地图组件**

默认选择 `v1`；标题栏显示旧地图/1.0 世界树分段按钮和在线人数；玩家坐标通过 `projectPlayerLocation()` 投影，`null` 坐标不渲染。

- [ ] **步骤 3：接入应用并整理样式**

从 `App.tsx` 删除旧的内联 `MapView` 与侧栏 `Wails + Go` 文案，导入新组件。为地图标题栏操作区、分段按钮、窄屏换行和地图标记增加稳定样式。

- [ ] **步骤 4：构建前端**

运行：`npm run build --prefix frontend`

预期：TypeScript 与 Vite 构建退出码为 0。

### 任务 3：完整验证

**文件：**
- 检查：所有本次修改文件

- [ ] **步骤 1：运行 Go 测试**

运行：`go test -count=1 ./...`

预期：退出码为 0。

- [ ] **步骤 2：检查补丁格式**

运行：`git diff --check`

预期：无空白错误。

- [ ] **步骤 3：构建 Wails 应用**

运行：`wails build`

预期：生成 `build/bin/palserver-launcher.exe`，退出码为 0。

- [ ] **步骤 4：检查最终差异和地图素材**

确认没有覆盖工作区中已有的 PalDefender 修改，两个地图文件可读取，地图组件默认选中 1.0 世界树地图。
