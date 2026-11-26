import { useEffect, useState } from "react";
import { login, type Room } from "./api";

// Live room list from the server. `connected` is false while the stream is down,
// so a projector never silently shows a stale state.
export function useRooms() {
  const [rooms, setRooms] = useState<Room[] | null>(null);
  const [connected, setConnected] = useState(true);

  useEffect(() => {
    let source: EventSource;
    let retry: number | undefined;
    let stopped = false;
    const connect = () => {
      source = new EventSource("/api/events");
      source.addEventListener("rooms", (e) => {
        setRooms(JSON.parse((e as MessageEvent).data));
        setConnected(true);
      });
      source.onerror = async () => {
        setConnected(false);
        if (stopped || source.readyState !== EventSource.CLOSED) return;
        // The browser gives up after a non-200 answer, e.g. when a restart wiped our session.
        const res = await fetch("/api/me").catch(() => null);
        if (res?.status === 401) return login();
        retry = window.setTimeout(connect, 3000);
      };
    };
    connect();
    return () => {
      stopped = true;
      source.close();
      clearTimeout(retry);
    };
  }, []);

  return { rooms, connected };
}

export function useMe() {
  const [name, setName] = useState<string | null>(null);
  useEffect(() => {
    fetch("/api/me")
      .then((r) => (r.ok ? r.json() : null))
      .then((me) => setName(me?.name ?? null))
      .catch(() => {});
  }, []);
  return name;
}

export function useNow(intervalMs: number) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs]);
  return now;
}

export function usePath() {
  const [path, setPath] = useState(location.pathname);
  useEffect(() => {
    const update = () => setPath(location.pathname);
    addEventListener("popstate", update);
    return () => removeEventListener("popstate", update);
  }, []);
  return path;
}

export function navigate(to: string) {
  history.pushState(null, "", to);
  dispatchEvent(new PopStateEvent("popstate"));
}
