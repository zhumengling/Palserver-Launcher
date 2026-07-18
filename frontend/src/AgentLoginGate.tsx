import { FormEvent, useCallback, useEffect, useState } from 'react';
import { KeyRound, RefreshCw, Server, ShieldCheck } from 'lucide-react';
import App from './App';
import { createAgentPassword, getAgentHealth, getAgentPlatform, isWebMode, loginAgent } from './platformApi';

export default function AgentLoginGate() {
  const [checking, setChecking] = useState(isWebMode);
  const [authenticated, setAuthenticated] = useState(!isWebMode);
  const [setupRequired, setSetupRequired] = useState(false);
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState('');
  const check = useCallback(async () => {
    if (!isWebMode) return;
    setChecking(true);
    try {
      const health = await getAgentHealth();
      setAuthenticated(health.authenticated);
      setSetupRequired(health.setupRequired);
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : String(nextError));
      setAuthenticated(false);
    } finally {
      setChecking(false);
    }
  }, []);
  useEffect(() => {
    void check();
    const requireLogin = () => { setAuthenticated(false); void check(); };
    window.addEventListener('pal-agent-auth-required', requireLogin);
    return () => window.removeEventListener('pal-agent-auth-required', requireLogin);
  }, [check]);
  async function submit(event: FormEvent) {
    event.preventDefault();
    setError('');
    setChecking(true);
    try {
      if (setupRequired) {
        if (password !== confirmPassword) throw new Error('两次输入的密码不一致');
        await createAgentPassword(password);
      } else {
        await loginAgent(password);
      }
      setPassword('');
      setConfirmPassword('');
      setSetupRequired(false);
      setAuthenticated(true);
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : String(nextError));
    } finally {
      setChecking(false);
    }
  }
  if (authenticated) return <App/>;
  const linux = getAgentPlatform() === 'linux';
  const submitDisabled = checking || password.length < (setupRequired ? 10 : 1) || setupRequired && confirmPassword.length < 10;
  return <main className="agent-login-shell"><form className="agent-login-card" onSubmit={submit}>
    <div className="agent-login-icon">{setupRequired ? <ShieldCheck size={28}/> : <Server size={28}/>}</div>
    <div><p className="eyebrow">Palserver {linux ? 'Linux' : 'Web'} Agent</p><h1>{setupRequired ? '创建管理密码' : '登录网页控制台'}</h1><p>{setupRequired ? '这是第一次打开网页控制台。请创建至少 10 个字符的管理密码，以后使用该密码登录。' : '输入首次使用时创建的管理密码。'}</p></div>
    <label><span>管理密码</span><div className="agent-password-input"><KeyRound size={16}/><input autoFocus type="password" autoComplete={setupRequired ? 'new-password' : 'current-password'} value={password} onChange={(event) => setPassword(event.target.value)} placeholder={setupRequired ? '至少 10 个字符' : '输入管理密码'}/></div></label>
    {setupRequired && <label><span>确认管理密码</span><div className="agent-password-input"><KeyRound size={16}/><input type="password" autoComplete="new-password" value={confirmPassword} onChange={(event) => setConfirmPassword(event.target.value)} placeholder="再次输入密码"/></div></label>}
    {error && <div className="agent-login-error">{error}</div>}
    <button className="primary" disabled={submitDisabled}>{checking ? <RefreshCw className="spin" size={16}/> : setupRequired ? <ShieldCheck size={16}/> : <KeyRound size={16}/>} {setupRequired ? '创建密码并进入控制台' : '登录'}</button>
    <small>远程访问建议通过 HTTPS、WireGuard 或 Tailscale，不要直接暴露 RCON 和 Palworld REST。</small>
  </form></main>;
}
