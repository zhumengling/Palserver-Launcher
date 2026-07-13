import { useEffect, useRef, useState } from 'react';
import { FileCode2, Save, SlidersHorizontal } from 'lucide-react';
import * as API from '../wailsjs/go/main/App';

type Run = (label: string, action: () => Promise<unknown>, success?: string) => Promise<void>;

const groups: Record<string, string[]> = {
  '帕鲁': ['PalCaptureRate','PalSpawnNumRate','PalDamageRateAttack','PalDamageRateDefense','PalStomachDecreaceRate','PalStaminaDecreaceRate','PalAutoHPRegeneRate','PalAutoHpRegeneRateInSleep','PalEggDefaultHatchingTime','WorkSpeedRate','bPalLost','bIsRandomizerPalLevelRandom','bAllowGlobalPalboxExport','bAllowGlobalPalboxImport','ServerReplicatePawnCullDistance'],
  '玩家': ['ExpRate','PlayerDamageRateAttack','PlayerDamageRateDefense','PlayerStomachDecreaceRate','PlayerStaminaDecreaceRate','PlayerAutoHPRegeneRate','PlayerAutoHpRegeneRateInSleep','bEnablePlayerToPlayerDamage','bEnableFriendlyFire','bIsPvP','bEnableFastTravel','bIsStartLocationSelectByMap','DeathPenalty','CoopPlayerMaxNum','ServerPlayerMaxNum','bShowPlayerList'],
  '公会与据点': ['BaseCampMaxNum','BaseCampWorkerMaxNum','BaseCampMaxNumInGuild','GuildPlayerMaxNum','bAutoResetGuildNoOnlinePlayers','AutoResetGuildTimeNoOnlinePlayers','bEnableDefenseOtherGuildPlayer','bCanPickupOtherGuildDeathPenaltyDrop','bInvisibleOtherGuildBaseCampAreaFX'],
  '建筑与掉落': ['BuildObjectHpRate','BuildObjectDamageRate','BuildObjectDeteriorationDamageRate','bBuildAreaLimit','MaxBuildingLimitNum','DropItemMaxNum','PhysicsActiveDropItemMaxNum','DropItemMaxNum_UNKO','CollectionDropRate','EnemyDropItemRate','DropItemAliveMaxHours','SupplyDropSpan','ItemWeightRate','EquipmentDurabilityDamageRate','ItemCorruptionMultiplier'],
  '服务器与跨平台': ['ServerDescription','Region','bUseAuth','BanListURL','bAllowClientMod','CrossplayPlatforms','ChatPostLimitPerMinute','LogFormatType','bIsShowJoinLeftMessage'],
  '1.0 新功能': ['EnablePredatorBossPal','MonsterFarmActionSpeedRate','DenyTechnologyList','GuildRejoinCooldownMinutes','AutoTransferMasterCheckIntervalSeconds','AutoTransferMasterThresholdDays','MaxGuildsPerFrame','BlockRespawnTime','RespawnPenaltyDurationThreshold','RespawnPenaltyTimeScale','bDisplayPvPItemNumOnWorldMap_BaseCamp','bDisplayPvPItemNumOnWorldMap_Player','AdditionalDropItemWhenPlayerKillingInPvPMode','AdditionalDropItemNumWhenPlayerKillingInPvPMode','bAdditionalDropItemWhenPlayerKillingInPvPMode','bEnableVoiceChat','VoiceChatMaxVolumeDistance','VoiceChatZeroVolumeDistance','bAllowEnhanceStat_Health','bAllowEnhanceStat_Attack','bAllowEnhanceStat_Stamina','bAllowEnhanceStat_Weight','bAllowEnhanceStat_WorkSpeed','bEnableBuildingPlayerUIdDisplay','BuildingNameDisplayCacheTTLSeconds','ItemContainerForceMarkDirtyInterval','PlayerDataPalStorageUpdateCheckTickInterval'],
  '世界与其他': ['DayTimeSpeedRate','NightTimeSpeedRate','CollectionObjectHpRate','CollectionObjectRespawnSpeedRate','bEnableInvaderEnemy','bActiveUNKO','bEnableAimAssistPad','bEnableAimAssistKeyboard','bIsMultiplay','bEnableNonLoginPenalty','bExistPlayerAfterLogout','AutoSaveSpan','RandomizerType','RandomizerSeed','bHardcore','bCharacterRecreateInHardcore','bEnableFastTravelOnlyBaseCamp','bIsUseBackupSaveData'],
};

const labels: Record<string, string> = {
  PalCaptureRate:'捕获率',PalSpawnNumRate:'帕鲁出现倍率',PalDamageRateAttack:'帕鲁攻击倍率',PalDamageRateDefense:'帕鲁承伤倍率',PalStomachDecreaceRate:'帕鲁饥饿速度',PalStaminaDecreaceRate:'帕鲁耐力消耗',PalAutoHPRegeneRate:'帕鲁生命恢复',PalAutoHpRegeneRateInSleep:'帕鲁睡眠恢复',PalEggDefaultHatchingTime:'巨大蛋孵化时间',WorkSpeedRate:'工作速度',ExpRate:'经验倍率',PlayerDamageRateAttack:'玩家攻击倍率',PlayerDamageRateDefense:'玩家承伤倍率',PlayerStomachDecreaceRate:'玩家饥饿速度',PlayerStaminaDecreaceRate:'玩家耐力消耗',PlayerAutoHPRegeneRate:'玩家生命恢复',PlayerAutoHpRegeneRateInSleep:'玩家睡眠恢复',DeathPenalty:'死亡惩罚',CoopPlayerMaxNum:'组队人数',ServerPlayerMaxNum:'最大玩家数',BaseCampMaxNum:'据点数量',BaseCampWorkerMaxNum:'据点工作帕鲁',BaseCampMaxNumInGuild:'公会据点数量',GuildPlayerMaxNum:'公会人数',BuildObjectDamageRate:'建筑伤害倍率',BuildObjectDeteriorationDamageRate:'建筑劣化倍率',DropItemMaxNum:'地图掉落上限',CollectionDropRate:'采集掉落倍率',EnemyDropItemRate:'敌人掉落倍率',DropItemAliveMaxHours:'掉落保留小时',SupplyDropSpan:'补给投放间隔',DayTimeSpeedRate:'白天速度',NightTimeSpeedRate:'夜晚速度',CollectionObjectHpRate:'采集物生命倍率',CollectionObjectRespawnSpeedRate:'采集物刷新倍率',AutoSaveSpan:'自动保存间隔',RandomizerType:'随机模式',RandomizerSeed:'随机种子',ServerReplicatePawnCullDistance:'同步距离',
};

const booleanKeys = new Set([...Object.values(groups).flat().filter((key) => key.startsWith('b')), 'RCONEnabled', 'RESTAPIEnabled', 'EnablePredatorBossPal']);
const stringKeys = new Set(['ServerName','ServerDescription','PublicIP','Region','BanListURL','CrossplayPlatforms','RandomizerSeed','DenyTechnologyList','AdditionalDropItemWhenPlayerKillingInPvPMode']);
const optionValues: Record<string, string[]> = { DeathPenalty: ['全部保留惩罚','无惩罚','仅物品','物品和装备'], RandomizerType: ['关闭','普通','高随机'], LogFormatType: ['文本','JSON'] };
const optionStorageValues: Record<string, string[]> = { DeathPenalty: ['All','None','Item','ItemAndEquipment'], RandomizerType: ['None','1','2'], LogFormatType: ['Text','Json'] };

const extraLabels: Record<string, string> = {
  bPalLost:'帕鲁死亡掉落', bIsRandomizerPalLevelRandom:'帕鲁等级随机化', bAllowGlobalPalboxExport:'允许跨界帕鲁终端导出', bAllowGlobalPalboxImport:'允许跨界帕鲁终端导入',
  bEnablePlayerToPlayerDamage:'允许玩家互相伤害', bEnableFriendlyFire:'允许队友伤害', bIsPvP:'启用 PvP', bEnableFastTravel:'允许快速旅行', bIsStartLocationSelectByMap:'允许地图选择出生点', bShowPlayerList:'显示玩家列表',
  bAutoResetGuildNoOnlinePlayers:'无在线玩家时重置公会', AutoResetGuildTimeNoOnlinePlayers:'公会重置等待时间', bEnableDefenseOtherGuildPlayer:'允许防御其他公会玩家', bCanPickupOtherGuildDeathPenaltyDrop:'允许拾取其他玩家掉落物', bInvisibleOtherGuildBaseCampAreaFX:'隐藏其他公会据点特效',
  bBuildAreaLimit:'限制建筑区域', DropItemAliveMaxHours:'掉落物保留时间', bEnableInvaderEnemy:'启用入侵敌人', bEnableAimAssistPad:'手柄瞄准辅助', bEnableAimAssistKeyboard:'键鼠瞄准辅助', bIsMultiplay:'启用多人游戏', bEnableNonLoginPenalty:'未登录惩罚', bExistPlayerAfterLogout:'退出后保留角色', bHardcore:'硬核模式', bIsUseBackupSaveData:'使用存档备份',
  BuildObjectHpRate:'建筑生命值倍率', PhysicsActiveDropItemMaxNum:'启用物理的掉落物上限', DropItemMaxNum_UNKO:'粪便掉落上限', bActiveUNKO:'启用粪便生成', bCharacterRecreateInHardcore:'硬核模式允许重建角色', bEnableFastTravelOnlyBaseCamp:'仅允许据点快速旅行', ItemWeightRate:'物品重量倍率',
  ServerName:'服务器名称', ServerDescription:'服务器说明', PublicIP:'公网 IP', PublicPort:'游戏端口', RCONEnabled:'启用 RCON', RCONPort:'RCON 端口', RESTAPIEnabled:'启用 REST API', RESTAPIPort:'REST API 端口', Region:'服务器地区', bUseAuth:'启用身份验证', BanListURL:'官方封禁列表地址', bAllowClientMod:'允许客户端模组', CrossplayPlatforms:'允许跨平台', ChatPostLimitPerMinute:'每分钟聊天上限', LogFormatType:'日志格式', bIsShowJoinLeftMessage:'显示玩家加入离开消息',
  EnablePredatorBossPal:'启用掠食者首领帕鲁', MaxBuildingLimitNum:'最大建筑数量', EquipmentDurabilityDamageRate:'装备耐久消耗倍率', ItemContainerForceMarkDirtyInterval:'容器强制同步间隔', PlayerDataPalStorageUpdateCheckTickInterval:'玩家帕鲁仓库检查间隔', ItemCorruptionMultiplier:'物品腐坏倍率', MonsterFarmActionSpeedRate:'牧场工作速度倍率', DenyTechnologyList:'禁用科技列表', GuildRejoinCooldownMinutes:'重新加入公会冷却分钟', AutoTransferMasterCheckIntervalSeconds:'自动转让会长检查间隔', AutoTransferMasterThresholdDays:'自动转让会长离线天数', MaxGuildsPerFrame:'每帧处理公会数量', BlockRespawnTime:'出生点封锁时间', RespawnPenaltyDurationThreshold:'复活惩罚触发阈值', RespawnPenaltyTimeScale:'复活惩罚时间倍率', bDisplayPvPItemNumOnWorldMap_BaseCamp:'地图显示据点 PvP 物品数', bDisplayPvPItemNumOnWorldMap_Player:'地图显示玩家 PvP 物品数', AdditionalDropItemWhenPlayerKillingInPvPMode:'PvP 击杀额外掉落物', AdditionalDropItemNumWhenPlayerKillingInPvPMode:'PvP 击杀额外掉落数量', bAdditionalDropItemWhenPlayerKillingInPvPMode:'启用 PvP 击杀额外掉落', bEnableVoiceChat:'启用语音聊天', VoiceChatMaxVolumeDistance:'语音最大音量距离', VoiceChatZeroVolumeDistance:'语音静音距离', bAllowEnhanceStat_Health:'允许强化生命', bAllowEnhanceStat_Attack:'允许强化攻击', bAllowEnhanceStat_Stamina:'允许强化耐力', bAllowEnhanceStat_Weight:'允许强化负重', bAllowEnhanceStat_WorkSpeed:'允许强化工作速度', bEnableBuildingPlayerUIdDisplay:'建筑显示玩家 UID', BuildingNameDisplayCacheTTLSeconds:'建筑名称缓存秒数',
};
Object.assign(labels, extraLabels);

const settingRanges: Record<string, [number, number, number]> = {
  DropItemMaxNum: [0, 10000, 100], PhysicsActiveDropItemMaxNum: [-1, 10000, 1], AutoResetGuildTimeNoOnlinePlayers: [0, 720, 1],
  PublicPort: [1, 65535, 1], RCONPort: [1, 65535, 1], RESTAPIPort: [1, 65535, 1], ChatPostLimitPerMinute: [0, 1000, 1],
  AutoTransferMasterCheckIntervalSeconds: [60, 86400, 60], AutoTransferMasterThresholdDays: [0, 365, 1], BuildingNameDisplayCacheTTLSeconds: [0, 3600, 1],
};

function settingRange(key: string): [number, number, number] {
  if (settingRanges[key]) return settingRanges[key];
  if (key.toLowerCase().includes('distance')) return [0, 20000, 100];
  if (key.toLowerCase().includes('maxnum') || key.toLowerCase().includes('playermax') || key.toLowerCase().includes('dropitemmax')) return [0, 1000, 1];
  if (key.toLowerCase().includes('hours') || key.toLowerCase().includes('span')) return [0, 240, 1];
  if (key.toLowerCase().includes('hatching')) return [0, 240, 0.1];
  return [0, 10, 0.1];
}

function settingVisible(key: string, values: Record<string,string>) {
  const enabled = (name: string) => (values[name] || 'False').toLowerCase() === 'true';
  if (['AutoResetGuildTimeNoOnlinePlayers'].includes(key)) return enabled('bAutoResetGuildNoOnlinePlayers');
  if (['bIsRandomizerPalLevelRandom','RandomizerSeed'].includes(key)) return (values.RandomizerType || 'None') !== 'None';
  if (key === 'bCharacterRecreateInHardcore') return enabled('bHardcore');
  if (key === 'bEnableFastTravelOnlyBaseCamp') return enabled('bEnableFastTravel');
  if (['VoiceChatMaxVolumeDistance','VoiceChatZeroVolumeDistance'].includes(key)) return enabled('bEnableVoiceChat');
  if (['bDisplayPvPItemNumOnWorldMap_BaseCamp','bDisplayPvPItemNumOnWorldMap_Player','bAdditionalDropItemWhenPlayerKillingInPvPMode'].includes(key)) return enabled('bIsPvP');
  if (['AdditionalDropItemWhenPlayerKillingInPvPMode','AdditionalDropItemNumWhenPlayerKillingInPvPMode'].includes(key)) return enabled('bIsPvP') && enabled('bAdditionalDropItemWhenPlayerKillingInPvPMode');
  return true;
}

function SettingNumber({ keyName, value, onChange }: { keyName: string; value: string; onChange: (value: string) => void }) {
  const [open, setOpen] = useState(false);
  const dragging = useRef(false);
  const [min, max, step] = settingRange(keyName);
  const parsed = Number(value);
  const sliderValue = Number.isFinite(parsed) ? Math.max(min, Math.min(max, parsed)) : min;
  const finishDrag = () => { dragging.current = false; setOpen(false); };
  return <div className="setting-number"><input type="number" step={step} min={min} max={max} value={value} onFocus={() => setOpen(true)} onChange={(event) => onChange(event.target.value)} onBlur={() => window.setTimeout(() => { if (!dragging.current) setOpen(false); }, 160)}/>{open && <div className="setting-slider-popover"><input type="range" min={min} max={max} step={step} value={sliderValue} onPointerDown={(event) => { dragging.current = true; event.currentTarget.setPointerCapture(event.pointerId); }} onPointerUp={finishDrag} onPointerCancel={finishDrag} onChange={(event) => onChange(event.target.value)}/><span>{sliderValue}</span></div>}</div>;
}

export default function WorldSettingsView({ id, running, run }: { id: string; running: boolean; run: Run }) {
  const [mode, setMode] = useState<'structured'|'raw'>('structured');
  const [values, setValues] = useState<Record<string,string>>({});
  const [raw, setRaw] = useState('');
  const load = async () => { setValues(await API.GetWorldSettingsValues(id)); setRaw(await API.ReadWorldSettings(id)); };
  useEffect(() => { load(); }, [id]);
  const saveStructured = () => run('save-world-settings', async () => { await API.SaveWorldSettingsValues(id, values); await load(); }, '世界设置已保存');
  const saveRaw = () => run('save-world-settings-raw', async () => { await API.WriteWorldSettings(id, raw); await load(); }, '原始配置已保存');
  return <section className="panel settings-panel">
    <div className="panel-heading"><div><h2>世界设置</h2><p>结构化编辑覆盖旧版全部设置，原始模式保留完整控制</p></div><div className="toolbar"><div className="segmented"><button className={mode === 'structured' ? 'active' : ''} onClick={() => setMode('structured')}><SlidersHorizontal size={14}/>结构化</button><button className={mode === 'raw' ? 'active' : ''} onClick={() => setMode('raw')}><FileCode2 size={14}/>原始配置</button></div><button className="primary" disabled={running} onClick={mode === 'structured' ? saveStructured : saveRaw}><Save size={15}/>保存</button></div></div>
    {running && <div className="inline-warning">停止服务器后才能保存设置。</div>}
    {mode === 'raw' ? <textarea className="code-editor" spellCheck={false} value={raw} onChange={(event) => setRaw(event.target.value)}/> : <div className="settings-groups">
      {Object.entries(groups).map(([group, keys]) => <section className="settings-group" key={group}><h3>{group}</h3><div className="settings-fields">{keys.filter((key) => settingVisible(key, values)).map((key) => <label key={key}><span><strong>{labels[key] || key}</strong><small>{key}</small></span>{booleanKeys.has(key) ? <input type="checkbox" checked={(values[key] || 'False').toLowerCase() === 'true'} onChange={(event) => setValues({ ...values, [key]: event.target.checked ? 'True' : 'False' })}/> : optionValues[key] ? <select value={optionStorageValues[key].indexOf(values[key] || optionStorageValues[key][0]) >= 0 ? optionValues[key][optionStorageValues[key].indexOf(values[key] || optionStorageValues[key][0])] : optionValues[key][0]} onChange={(event) => { const index = optionValues[key].indexOf(event.target.value); setValues({ ...values, [key]: optionStorageValues[key][index] || event.target.value }); }}>{optionValues[key].map((option) => <option key={option}>{option}</option>)}</select> : stringKeys.has(key) ? <input type="text" value={values[key] || ''} onChange={(event) => setValues({ ...values, [key]: event.target.value })}/> : <SettingNumber keyName={key} value={values[key] || ''} onChange={(next) => setValues({ ...values, [key]: next })}/>}</label>)}</div></section>)}
    </div>}
  </section>;
}
