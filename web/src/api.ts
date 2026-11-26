export type Room = {
  id: string;
  name: string;
  occupied: boolean;
  by?: string;
  since?: string;
};

export function login() {
  location.assign(`/auth/login?next=${encodeURIComponent(location.pathname)}`);
}

async function send(method: string, path: string, body?: unknown) {
  const res = await fetch(path, {
    method,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 401) login();
  if (!res.ok) throw new Error((await res.text()).trim() || res.statusText);
  return res;
}

export const createRoom = (name: string) => send("POST", "/api/rooms", { name });
export const deleteRoom = (id: string) => send("DELETE", `/api/rooms/${id}`);
export const setOccupied = (id: string, occupied: boolean) =>
  send("PUT", `/api/rooms/${id}/occupied`, { occupied });
