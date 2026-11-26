import { useCallback, useEffect, useState } from "react";
import { setOccupied, type Room } from "./api";
import { useNow } from "./hooks";
import { ConnectionBanner, Link } from "./Link";

type Props = { id: string; rooms: Room[] | null; connected: boolean };

export function RoomView({ id, rooms, connected }: Props) {
  const room = rooms?.find((r) => r.id === id);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const toggle = useCallback(async () => {
    if (!room) return;
    setBusy(true);
    try {
      // Send the wanted state, not "toggle": two people pressing at once end up in the same state.
      await setOccupied(room.id, !room.occupied);
      setError("");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }, [room]);

  // Space bar toggles, handy on the lectern laptop that drives the projector.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.code !== "Space" || e.target instanceof HTMLButtonElement || e.target instanceof HTMLInputElement) return;
      e.preventDefault();
      toggle();
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [toggle]);

  useEffect(() => {
    document.title = room ? `${room.occupied ? "🔴" : "🟢"} ${room.name}` : "Klo-Ampel";
    return () => {
      document.title = "Klo-Ampel";
    };
  }, [room]);

  if (rooms === null) return <main className="room" />;
  if (!room) {
    return (
      <main className="list">
        <p>This room no longer exists.</p>
        <Link to="/">Back to all rooms</Link>
      </main>
    );
  }

  return (
    <main className={`room ${room.occupied ? "occupied" : "free"}`}>
      <nav className="bar">
        <Link to="/">← All rooms</Link>
        {document.fullscreenEnabled && (
          <button className="link" onClick={toggleFullscreen}>
            Fullscreen
          </button>
        )}
      </nav>
      {!connected && <ConnectionBanner />}

      <div className="center">
        <h1 className="room-name">{room.name}</h1>
        <p className="state">{room.occupied ? "Occupied" : "Free"}</p>
        <p className="since">{room.occupied && room.since ? <Since by={room.by} since={room.since} /> : " "}</p>
      </div>

      <button className="toggle" onClick={toggle} disabled={busy}>
        {room.occupied ? "Release" : "Occupy"}
      </button>
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
    </main>
  );
}

function Since({ by, since }: { by?: string; since: string }) {
  const now = useNow(15_000);
  const start = new Date(since);
  const minutes = Math.max(0, Math.floor((now - start.getTime()) / 60_000));
  const time = start.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  return (
    <>
      by {by} · since {time} ({minutes} min)
    </>
  );
}

function toggleFullscreen() {
  if (document.fullscreenElement) document.exitFullscreen();
  else document.documentElement.requestFullscreen();
}
