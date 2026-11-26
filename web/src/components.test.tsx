import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { RoomList } from "./RoomList";
import { RoomView } from "./RoomView";
import type { Room } from "./api";

const free: Room = { id: "a1", name: "Toilet 1st floor", occupied: false };
const occupied: Room = { id: "b2", name: "Toilet 2nd floor", occupied: true, by: "Ada Lovelace", since: new Date().toISOString() };

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn(async (url: string) =>
    url === "/api/me" ? Response.json({ name: "Ada Lovelace" }) : new Response(null, { status: 200 }),
  );
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const lastCall = () => {
  const [url, init] = fetchMock.mock.calls.at(-1)!;
  return { url, method: init?.method, body: init?.body && JSON.parse(init.body) };
};

describe("RoomView", () => {
  it("shows a free room and occupies it", async () => {
    render(<RoomView id="a1" rooms={[free]} connected />);
    expect(screen.getByText("Free")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Occupy" }));
    await waitFor(() => expect(lastCall()).toEqual({ url: "/api/rooms/a1/occupied", method: "PUT", body: { occupied: true } }));
  });

  it("shows who occupied a room and releases it with the space bar", async () => {
    render(<RoomView id="b2" rooms={[occupied]} connected />);
    expect(screen.getByText("Occupied")).toBeTruthy();
    expect(screen.getByText(/by Ada Lovelace · since/)).toBeTruthy();
    fireEvent.keyDown(window, { code: "Space" });
    await waitFor(() => expect(lastCall().body).toEqual({ occupied: false }));
  });

  it("warns when the live connection is down", () => {
    render(<RoomView id="a1" rooms={[free]} connected={false} />);
    expect(screen.getByRole("status").textContent).toMatch(/Connection lost/);
  });

  it("handles a deleted room", () => {
    render(<RoomView id="gone" rooms={[free]} connected />);
    expect(screen.getByText("This room no longer exists.")).toBeTruthy();
  });
});

describe("RoomList", () => {
  it("lists rooms with their state", async () => {
    render(<RoomList rooms={[free, occupied]} connected />);
    expect(screen.getByText("Free")).toBeTruthy();
    expect(screen.getByText("Occupied by Ada Lovelace")).toBeTruthy();
    expect(await screen.findByText("Ada Lovelace")).toBeTruthy(); // logged-in user in the header
  });

  it("creates a room", async () => {
    render(<RoomList rooms={[]} connected />);
    fireEvent.change(screen.getByLabelText("Room name"), { target: { value: "Toilet 3rd floor" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    await waitFor(() => expect(lastCall()).toEqual({ url: "/api/rooms", method: "POST", body: { name: "Toilet 3rd floor" } }));
  });

  it("deletes a room only after confirmation", async () => {
    const confirm = vi.fn(() => false);
    vi.stubGlobal("confirm", confirm);
    render(<RoomList rooms={[free]} connected />);
    const button = screen.getByRole("button", { name: "Delete Toilet 1st floor" });

    fireEvent.click(button);
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === "DELETE")).toBe(false);

    confirm.mockReturnValue(true);
    fireEvent.click(button);
    await waitFor(() => expect(lastCall()).toEqual({ url: "/api/rooms/a1", method: "DELETE", body: undefined }));
  });
});
