import { usePath, useRooms } from "./hooks";
import { RoomList } from "./RoomList";
import { RoomView } from "./RoomView";

export function App() {
  const path = usePath();
  const { rooms, connected } = useRooms();
  const roomId = path.match(/^\/rooms\/([^/]+)$/)?.[1];
  return roomId ? (
    <RoomView id={roomId} rooms={rooms} connected={connected} />
  ) : (
    <RoomList rooms={rooms} connected={connected} />
  );
}
