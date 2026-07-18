import { EventsOn as DesktopEventsOn } from '../wailsjs/runtime/runtime';
import { isWebMode } from './platformApi';

type Listener = (...args: any[]) => void;
const webListeners = new Map<string, Set<Listener>>();
let eventSource: EventSource | null = null;

function ensureEventSource() {
  if (!isWebMode || eventSource) return;
  eventSource = new EventSource('/api/v1/events', { withCredentials: true });
  eventSource.onmessage = (message) => {
    try {
      const event = JSON.parse(message.data) as { name: string; args?: any[] };
      webListeners.get(event.name)?.forEach((listener) => listener(...(event.args || [])));
    } catch {
      // Ignore a malformed frame without breaking later events.
    }
  };
  eventSource.onerror = () => {
    eventSource?.close(); eventSource = null; window.setTimeout(ensureEventSource, 3000);
  };
}

export function EventsOn(name: string, listener: Listener) {
  if (!isWebMode) return DesktopEventsOn(name, listener);
  const listeners = webListeners.get(name) || new Set<Listener>();
  listeners.add(listener); webListeners.set(name, listeners); ensureEventSource();
  return () => { listeners.delete(listener); if (!listeners.size) webListeners.delete(name); };
}
