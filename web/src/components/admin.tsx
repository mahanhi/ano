"use client";

import { useEffect, useState } from "react";
import { ChevronRight } from "lucide-react";

type AdminDevice = {
  id: string;
  name: string;
  kind: string;
  room: string;
  addr: string;
  mac: string;
  names: number;
  bytesLeft: number;
  bytesKept: number;
  protocols: string;
  onNetwork: boolean;
  viaAno: boolean;
};

type AdminGroup = { kind: string; title: string; devices: AdminDevice[] };

type AdminView = {
  routerAddr: string;
  routerAt: string;
  connected: boolean;
  detail: string;
  mode: string;
  groups: AdminGroup[];
  viaAddr: string;
  viaPort: number;
  proxyReady: boolean;
  dnsReady: boolean;
  dnsDetail: string;
  steerOn: boolean;
  steerDetail: string;
  reviewOn: boolean;
  reviewEvery: number;
  reviewUnit: string;
  reviewHour: number;
  reviewMinute: number;
  reviewWeekday: number;
  reviewMonthDay: number;
  reviewLast: string;
  reviewDetail: string;
};

function bytes(n: number) {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)} MB`;
  if (n >= 1000) return `${(n / 1000).toFixed(1)} KB`;
  return `${n} B`;
}

export function AdminPage({ open }: { open: (id: string) => void }) {
  const [admin, setAdmin] = useState<AdminView | null>(null);
  const [routes, setRoutes] = useState(0);
  const [busy, setBusy] = useState(false);
  const [dnsBusy, setDnsBusy] = useState(false);
  const [steerBusy, setSteerBusy] = useState(false);
  const [reviewBusy, setReviewBusy] = useState(false);
  const [busyId, setBusyId] = useState("");
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState("");

  async function load() {
    const response = await fetch("/api/admin", { cache: "no-store" });
    const body = (await response.json()) as { admin?: AdminView; envoy?: { routes: number }; error?: string };
    if (!response.ok || !body.admin) {
      setError(body.error || "Ano could not read the regulator");
      return;
    }
    setAdmin(body.admin);
    setRoutes(body.envoy?.routes ?? 0);
    setLoaded(true);
    setError("");
  }

  useEffect(() => {
    const pull = () => {
      load().catch((err: Error) => setError(err.message));
    };
    pull();
    const timer = setInterval(pull, 3000);
    return () => clearInterval(timer);
  }, []);

  async function connect() {
    setBusy(true);
    setError("");
    try {
      const response = await fetch("/api/admin/connect", { method: "POST" });
      const body = (await response.json()) as { admin?: AdminView; envoy?: { routes: number }; error?: string };
      if (!response.ok || !body.admin) {
        setError(body.error || "Could not connect to the home router");
        return;
      }
      setAdmin(body.admin);
      setRoutes(body.envoy?.routes ?? 0);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not connect to the home router");
    } finally {
      setBusy(false);
    }
  }

  async function setVia(id: string, on: boolean) {
    setBusyId(id);
    setError("");
    try {
      const response = await fetch(`/api/devices/${id}/via`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ on }),
      });
      const body = (await response.json()) as { admin?: AdminView; error?: string };
      if (!response.ok || !body.admin) {
        setError(body.error || "Ano could not add that device");
        return;
      }
      setAdmin(body.admin);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ano could not add that device");
    } finally {
      setBusyId("");
    }
  }

  async function setSteer(on: boolean) {
    setSteerBusy(true);
    setError("");
    try {
      const response = await fetch("/api/admin/steer", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ on }),
      });
      const body = (await response.json()) as { admin?: AdminView; error?: string };
      if (!response.ok || !body.admin) {
        setError(body.error || "Ano could not take the Wi-Fi path");
        return;
      }
      setAdmin(body.admin);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ano could not take the Wi-Fi path");
    } finally {
      setSteerBusy(false);
    }
  }

  async function saveReview(schedule: ScheduleDraft) {
    setReviewBusy(true);
    setError("");
    try {
      const response = await fetch("/api/admin/review-schedule", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(schedule),
      });
      const body = (await response.json()) as { admin?: AdminView; error?: string };
      if (!response.ok || !body.admin) {
        setError(body.error || "Ano could not save the schedule");
        return;
      }
      setAdmin(body.admin);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ano could not save the schedule");
    } finally {
      setReviewBusy(false);
    }
  }

  async function reviewNow() {
    setReviewBusy(true);
    setError("");
    try {
      const response = await fetch("/api/admin/review", { method: "POST" });
      const body = (await response.json()) as { admin?: AdminView; error?: string };
      if (body.admin) setAdmin(body.admin);
      if (!response.ok) {
        setError(body.error || "AI Inference did not review today's traffic");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "AI Inference did not review today's traffic");
    } finally {
      setReviewBusy(false);
    }
  }

  async function allowDNS() {
    setDnsBusy(true);
    setError("");
    try {
      const response = await fetch("/api/admin/dns", { method: "POST" });
      const body = (await response.json()) as { admin?: AdminView; error?: string };
      if (!response.ok || !body.admin) {
        setError(body.error || "Ano could not open DNS");
        return;
      }
      setAdmin(body.admin);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ano could not open DNS");
    } finally {
      setDnsBusy(false);
    }
  }

  if (!loaded && !error) {
    return (
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Admin</h1>
        <p className="mt-1 text-sm text-muted-foreground">Reading the home network…</p>
      </div>
    );
  }

  const devices = admin?.groups.flatMap((group) => group.devices) ?? [];
  const left = devices.reduce((sum, device) => sum + device.bytesLeft, 0);
  const kept = devices.reduce((sum, device) => sum + device.bytesKept, 0);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Admin</h1>
          <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">
            Add a device to send it through Ano. Streaming stays direct. Passive watches. Active applies Refuse and Send.
          </p>
        </div>
        <button
          type="button"
          onClick={connect}
          disabled={busy}
          className="h-9 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground disabled:opacity-60"
        >
          {busy ? "Reading the network…" : "Refresh network"}
        </button>
      </div>
      {error ? <p className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm">{error}</p> : null}
      <Schedule admin={admin} busy={reviewBusy} onSave={saveReview} onRun={reviewNow} />
      <DropList />
      <Path admin={admin} dnsBusy={dnsBusy} steerBusy={steerBusy} onDNS={allowDNS} onSteer={setSteer} />
      <section className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Fact label="Router" value={admin?.connected ? admin.routerAddr : "Reading"} note={admin?.detail || "Ano is reading the Wi-Fi and the router."} />
        <Fact label="Regulator" value={admin?.mode === "active" ? "Active" : "Passive"} note={admin?.mode === "active" ? `${routes} enforced routes` : "Collecting. Nothing is enforced."} />
        <Fact label="Devices" value={String(devices.length)} note="Grouped by kind" />
        <Fact label="Traffic" value={bytes(left)} note={`${bytes(kept)} kept after a refusal`} />
      </section>
      {admin?.groups.map((group) => (
        <section key={group.kind}>
          <h2 className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">{group.title}</h2>
          <ul className="mt-2 space-y-2">
            {group.devices.map((device) => (
              <li key={device.id} className="rounded-xl border bg-card px-4 py-3.5">
                <div className="flex items-center gap-3">
                  <button type="button" onClick={() => open(device.id)} className="min-w-0 flex-1 text-left">
                    <span className="flex flex-wrap items-baseline justify-between gap-2">
                      <span className="text-sm font-medium">{device.name}</span>
                      <span className="text-[11px] text-muted-foreground">{device.room}</span>
                    </span>
                    <span className="mt-1 block text-[12px] text-muted-foreground">
                      {device.onNetwork ? `${singularKind(device.kind)} · ${device.addr}${device.mac ? ` · ${device.mac}` : ""}` : "Not seen on the router yet"}
                      {device.protocols ? ` · ${device.protocols}` : ""}
                      {device.viaAno ? " · Through Ano" : ""}
                    </span>
                    <span className="mt-1 block text-[12px] text-muted-foreground">{trafficLine(device)}</span>
                  </button>
                  <button
                    type="button"
                    disabled={busyId === device.id}
                    onClick={() => setVia(device.id, !device.viaAno)}
                    className={
                      device.viaAno
                        ? "h-8 shrink-0 rounded-md border px-2.5 text-[12px] font-medium text-muted-foreground"
                        : "h-8 shrink-0 rounded-md bg-primary px-2.5 text-[12px] font-medium text-primary-foreground disabled:opacity-60"
                    }
                  >
                    {busyId === device.id ? "Saving…" : device.viaAno ? "Remove" : "Add to Ano"}
                  </button>
                  <button
                    type="button"
                    onClick={() => open(device.id)}
                    className="inline-flex shrink-0 items-center gap-0.5 rounded-md border border-primary/30 bg-primary/10 px-2 py-1 text-[12px] font-medium text-primary"
                  >
                    Open
                    <ChevronRight className="size-3.5" />
                  </button>
                </div>
                {device.viaAno ? <p className="mt-2 text-[12px] leading-5 text-muted-foreground">{addedHint(device, admin)}</p> : null}
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}

function localWhen(value: string) {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toLocaleString();
}

function singularKind(kind: string) {
  if (kind === "speaker") return "Speaker";
  if (kind === "phone") return "Phone";
  if (kind === "tv") return "TV";
  if (kind === "camera") return "Camera";
  if (kind === "garage") return "Garage";
  if (kind === "light") return "Light";
  if (kind === "printer") return "Printer";
  if (kind === "thermostat") return "Thermostat";
  if (kind === "computer") return "Computer";
  if (kind === "unknown") return "Device";
  return kind || "Device";
}

function trafficLine(device: AdminDevice) {
  if (device.names === 0) {
    return device.viaAno ? "Added. Its own packets still go to the router." : "No bytes seen yet";
  }
  return `${device.names} name${device.names === 1 ? "" : "s"} · seen ${bytes(device.bytesLeft)} · kept ${bytes(device.bytesKept)}`;
}

function addedHint(device: AdminDevice, admin: AdminView | null) {
  if (!device.viaAno) {
    return "Ano records the conversations it can see with this device.";
  }
  if (admin?.steerOn) {
    return "A redirect is still on. Return added devices to the router.";
  }
  return "Added. Its own packets still go to the router. That redirect stays off.";
}

type ScheduleDraft = {
  on: boolean;
  every: number;
  unit: string;
  hour: number;
  minute: number;
  weekday: number;
  monthDay: number;
};

const weekdays = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

function Schedule({
  admin,
  busy,
  onSave,
  onRun,
}: {
  admin: AdminView | null;
  busy: boolean;
  onSave: (schedule: ScheduleDraft) => void;
  onRun: () => void;
}) {
  const [on, setOn] = useState(true);
  const [every, setEvery] = useState("1");
  const [unit, setUnit] = useState("day");
  const [time, setTime] = useState("03:00");
  const [minuteOfHour, setMinuteOfHour] = useState("0");
  const [weekday, setWeekday] = useState("0");
  const [monthDay, setMonthDay] = useState("1");
  useEffect(() => {
    if (!admin) return;
    setOn(admin.reviewOn);
    setEvery(String(admin.reviewEvery || 1));
    setUnit(admin.reviewUnit || "day");
    setTime(`${String(admin.reviewHour).padStart(2, "0")}:${String(admin.reviewMinute).padStart(2, "0")}`);
    setMinuteOfHour(String(admin.reviewMinute));
    setWeekday(String(admin.reviewWeekday));
    setMonthDay(String(admin.reviewMonthDay || 1));
  }, [admin?.reviewOn, admin?.reviewEvery, admin?.reviewUnit, admin?.reviewHour, admin?.reviewMinute, admin?.reviewWeekday, admin?.reviewMonthDay]);
  const parts = time.split(":");
  const hour = Number(parts[0]);
  const clockMinute = Number(parts[1]);
  const count = Number(every);
  const atMinute = unit === "hour" ? Number(minuteOfHour) : clockMinute;
  const draft: ScheduleDraft = {
    on,
    every: count,
    unit,
    hour: Number.isInteger(hour) ? hour : 3,
    minute: Number.isInteger(atMinute) ? atMinute : 0,
    weekday: Number(weekday),
    monthDay: Number(monthDay),
  };
  const limits: Record<string, number> = { minute: 59, hour: 24, day: 31, week: 52, month: 12 };
  const valid = Number.isInteger(count) && count >= 1 && count <= (limits[unit] ?? 1) && draft.hour >= 0 && draft.hour <= 23 && draft.minute >= 0 && draft.minute <= 59;
  return (
    <section className="rounded-xl border bg-card px-4 py-3.5">
      <h2 className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">Schedule</h2>
      <p className="mt-2 max-w-3xl text-sm leading-6 text-muted-foreground">
        Reviews the past 24 hours on this schedule. An earlier choice stays.
      </p>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <button
          type="button"
          disabled={busy}
          onClick={() => onSave({ ...draft, on: !on })}
          className={on ? "h-9 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground disabled:opacity-60" : "h-9 rounded-md border px-3 text-sm font-medium disabled:opacity-60"}
        >
          {on ? "On" : "Off"}
        </button>
        <label className="flex items-center gap-2 text-sm text-muted-foreground">
          Every
          <input
            type="number"
            min={1}
            max={limits[unit] ?? 1}
            value={every}
            onChange={(event) => setEvery(event.target.value)}
            className="h-9 w-20 rounded-md border bg-background px-2 text-sm text-foreground"
          />
        </label>
        <select value={unit} onChange={(event) => setUnit(event.target.value)} className="h-9 rounded-md border bg-background px-2 text-sm text-foreground">
          <option value="minute">Minute</option>
          <option value="hour">Hour</option>
          <option value="day">Day</option>
          <option value="week">Week</option>
          <option value="month">Month</option>
        </select>
        {unit === "hour" ? (
          <label className="flex items-center gap-2 text-sm text-muted-foreground">
            At minute
            <input
              type="number"
              min={0}
              max={59}
              value={minuteOfHour}
              onChange={(event) => setMinuteOfHour(event.target.value)}
              className="h-9 w-20 rounded-md border bg-background px-2 text-sm text-foreground"
            />
          </label>
        ) : null}
        {unit === "week" ? (
          <select value={weekday} onChange={(event) => setWeekday(event.target.value)} className="h-9 rounded-md border bg-background px-2 text-sm text-foreground">
            {weekdays.map((name, index) => (
              <option key={name} value={index}>
                {name}
              </option>
            ))}
          </select>
        ) : null}
        {unit === "month" ? (
          <label className="flex items-center gap-2 text-sm text-muted-foreground">
            Day
            <input
              type="number"
              min={1}
              max={28}
              value={monthDay}
              onChange={(event) => setMonthDay(event.target.value)}
              className="h-9 w-20 rounded-md border bg-background px-2 text-sm text-foreground"
            />
          </label>
        ) : null}
        {unit === "day" || unit === "week" || unit === "month" ? (
          <label className="flex items-center gap-2 text-sm text-muted-foreground">
            At
            <input type="time" value={time} onChange={(event) => setTime(event.target.value)} className="h-9 rounded-md border bg-background px-2 text-sm text-foreground" />
          </label>
        ) : null}
        <button type="button" disabled={busy || !valid} onClick={() => onSave(draft)} className="h-9 rounded-md border px-3 text-sm font-medium disabled:opacity-60">
          {busy ? "Saving…" : "Save"}
        </button>
        <button type="button" disabled={busy} onClick={onRun} className="h-9 rounded-md border px-3 text-sm font-medium disabled:opacity-60">
          Review now
        </button>
      </div>
      <p className="mt-3 text-[13px] leading-5 text-muted-foreground">
        {scheduleLine(draft)}
        {admin?.reviewLast ? ` Last run ${localWhen(admin.reviewLast)}.` : ""}
        {admin?.reviewDetail ? ` ${admin.reviewDetail}` : ""}
      </p>
    </section>
  );
}

function scheduleLine(draft: ScheduleDraft) {
  if (!draft.on) return "The schedule is off.";
  const count = draft.every === 1 ? "" : `${draft.every} `;
  const names: Record<string, [string, string]> = {
    minute: ["minute", "minutes"],
    hour: ["hour", "hours"],
    day: ["day", "days"],
    week: ["week", "weeks"],
    month: ["month", "months"],
  };
  const pair = names[draft.unit] ?? names.day;
  const unit = draft.every === 1 ? pair[0] : pair[1];
  const clock = `${String(draft.hour).padStart(2, "0")}:${String(draft.minute).padStart(2, "0")}`;
  if (draft.unit === "minute") return `Every ${count}${unit}.`;
  if (draft.unit === "hour") return `Every ${count}${unit} at minute ${draft.minute}.`;
  if (draft.unit === "week") return `Every ${count}${unit} on ${weekdays[draft.weekday] ?? "Sunday"} at ${clock}.`;
  if (draft.unit === "month") return `Every ${count}${unit} on day ${draft.monthDay} at ${clock}.`;
  return `Every ${count}${unit} at ${clock}.`;
}

type DropRow = { id: string; name: string; port: number; source: string };

function DropList() {
  const [drops, setDrops] = useState<DropRow[]>([]);
  const [name, setName] = useState("");
  const [allPorts, setAllPorts] = useState(true);
  const [port, setPort] = useState("443");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function load() {
    const response = await fetch("/api/admin/blocks", { cache: "no-store" });
    const body = (await response.json()) as { drops?: DropRow[]; error?: string };
    if (!response.ok) {
      setError(body.error || "Ano could not read the drop list");
      return;
    }
    setDrops(body.drops ?? []);
  }

  useEffect(() => {
    const pull = () => {
      load().catch((err: Error) => setError(err.message));
    };
    pull();
    const timer = setInterval(pull, 3000);
    return () => clearInterval(timer);
  }, []);

  async function add() {
    const chosen = allPorts ? 0 : Number(port);
    if (!allPorts && (!Number.isInteger(chosen) || chosen < 1 || chosen > 65535)) {
      setError("Choose all ports, or one port from 1 to 65535.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const response = await fetch("/api/admin/blocks", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, port: chosen }),
      });
      const body = (await response.json()) as { drops?: DropRow[]; error?: string };
      if (!response.ok) {
        setError(body.error || "Ano could not add that name");
        return;
      }
      setDrops(body.drops ?? []);
      setName("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ano could not add that name");
    } finally {
      setBusy(false);
    }
  }

  async function remove(id: string) {
    setBusy(true);
    setError("");
    try {
      const response = await fetch(`/api/admin/blocks/${encodeURIComponent(id)}`, { method: "DELETE" });
      const body = (await response.json()) as { drops?: DropRow[]; error?: string };
      if (!response.ok) {
        setError(body.error || "Ano could not remove that name");
        return;
      }
      setDrops(body.drops ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ano could not remove that name");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="rounded-xl border bg-card px-4 py-3.5">
      <h2 className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">Drop on Ano</h2>
      <p className="mt-2 max-w-3xl text-sm leading-6 text-muted-foreground">
        A name, on all ports or one port. All ports refuses the name. One port drops only that port.
      </p>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <label className="flex items-center gap-2 text-sm text-muted-foreground">
          DNS name
          <input
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="ads.example.com"
            className="h-9 w-56 rounded-md border bg-background px-2 text-sm text-foreground"
          />
        </label>
        <label className="flex items-center gap-2 text-sm text-muted-foreground">
          Ports
          <select
            value={allPorts ? "all" : "one"}
            onChange={(event) => setAllPorts(event.target.value === "all")}
            className="h-9 rounded-md border bg-background px-2 text-sm text-foreground"
          >
            <option value="all">All ports</option>
            <option value="one">One port</option>
          </select>
        </label>
        {allPorts ? null : (
          <label className="flex items-center gap-2 text-sm text-muted-foreground">
            Port
            <input
              type="number"
              min={1}
              max={65535}
              value={port}
              onChange={(event) => setPort(event.target.value)}
              className="h-9 w-24 rounded-md border bg-background px-2 text-sm text-foreground"
            />
          </label>
        )}
        <button type="button" disabled={busy || name.trim() === ""} onClick={add} className="h-9 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground disabled:opacity-60">
          {busy ? "Saving…" : "Add"}
        </button>
      </div>
      {error ? <p className="mt-3 text-sm text-destructive">{error}</p> : null}
      {drops.length === 0 ? (
        <p className="mt-3 text-[13px] leading-5 text-muted-foreground">No DNS names are listed for drop.</p>
      ) : (
        <ul className="mt-3 divide-y divide-border rounded-md border">
          {drops.map((drop) => (
            <li key={`${drop.source}-${drop.id}`} className="flex items-center justify-between gap-3 px-3 py-2 text-sm">
              <span className="min-w-0 truncate">{drop.name}</span>
              <span className="text-muted-foreground">{drop.port === 0 ? "All ports" : `Port ${drop.port}`}</span>
              {drop.source === "admin" ? (
                <button type="button" disabled={busy} onClick={() => remove(drop.id)} className="h-8 rounded-md border px-2 text-sm disabled:opacity-60">
                  Remove
                </button>
              ) : (
                <span className="text-muted-foreground">Refuse</span>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function Path({
  admin,
  dnsBusy,
  steerBusy,
  onDNS,
  onSteer,
}: {
  admin: AdminView | null;
  dnsBusy: boolean;
  steerBusy: boolean;
  onDNS: () => void;
  onSteer: (on: boolean) => void;
}) {
  const added = admin?.groups.flatMap((group) => group.devices).filter((device) => device.viaAno).length ?? 0;
  return (
    <section className="rounded-xl border bg-card px-4 py-3.5">
      <h2 className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">Through Ano</h2>
      <p className="mt-2 max-w-3xl text-sm leading-6 text-muted-foreground">
        {admin?.steerDetail || "Outgoing packets stay on the router. Ano leaves that redirect off."}{" "}
        {admin?.viaAddr ? `${added} device${added === 1 ? "" : "s"} added.` : "Ano is waiting for the home network address."}
      </p>
      <div className="mt-3 flex flex-wrap gap-2">
        {admin?.steerOn ? (
          <button
            type="button"
            onClick={() => onSteer(false)}
            disabled={steerBusy}
            className="h-9 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground disabled:opacity-60"
          >
            {steerBusy ? "Waiting for permission…" : "Return added devices to the router"}
          </button>
        ) : null}
        {admin && !admin.dnsReady ? (
          <button
            type="button"
            onClick={onDNS}
            disabled={dnsBusy}
            className="h-9 rounded-md border px-3 text-sm font-medium disabled:opacity-60"
          >
            {dnsBusy ? "Waiting for permission…" : "Allow DNS"}
          </button>
        ) : null}
      </div>
    </section>
  );
}

function Fact({ label, value, note }: { label: string; value: string; note: string }) {
  return (
    <div className="rounded-xl border bg-card px-4 py-3.5">
      <p className="text-[12px] text-muted-foreground">{label}</p>
      <p className="mt-2 text-base font-semibold tracking-tight">{value}</p>
      <p className="mt-2 text-[12px] leading-4 text-muted-foreground">{note}</p>
    </div>
  );
}
