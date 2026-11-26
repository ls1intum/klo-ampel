import { useState, type FormEvent } from "react";
import { createRoom, deleteRoom, type Room } from "./api";
import { useMe } from "./hooks";
import { ConnectionBanner, Link } from "./Link";

type Props = { rooms: Room[] | null; connected: boolean };

export function RoomList({ rooms, connected }: Props) {
  const me = useMe();
  const [name, setName] = useState("");
  const [error, setError] = useState("");

  const create = async (e: FormEvent) => {
    e.preventDefault();
    try {
      await createRoom(name);
      setName("");
      setError("");
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const remove = async (room: Room) => {
    if (!confirm(`Delete "${room.name}"? Every screen showing it will lose it.`)) return;
    try {
      await deleteRoom(room.id);
    } catch (err) {
      setError((err as Error).message);
    }
  };

  return (
    <main className="list">
      <header>
        <h1>
          <span className="logo" aria-hidden /> Klo-Ampel
        </h1>
        {me && (
          <form method="post" action="/auth/logout" className="user">
            <span>{me}</span>
            <button className="link">Log out</button>
          </form>
        )}
      </header>
      {!connected && <ConnectionBanner />}

      <form onSubmit={create} className="create">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="New room, e.g. Toilet 1st floor"
          aria-label="Room name"
          maxLength={60}
          required
        />
        <button>Create</button>
      </form>
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}

      {rooms === null ? (
        <p className="muted">Loading…</p>
      ) : rooms.length === 0 ? (
        <p className="muted">No rooms yet. Create one above.</p>
      ) : (
        <ul className="rooms">
          {rooms.map((room) => (
            <li key={room.id} className={room.occupied ? "occupied" : "free"}>
              <Link to={`/rooms/${room.id}`} className="room-link">
                <span className="name">{room.name}</span>
                <span className="status">{room.occupied ? `Occupied by ${room.by}` : "Free"}</span>
              </Link>
              <button className="delete" aria-label={`Delete ${room.name}`} onClick={() => remove(room)}>
                ✕
              </button>
            </li>
          ))}
        </ul>
      )}
    </main>
  );
}
