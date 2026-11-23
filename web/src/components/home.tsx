"use client";

import { Fragment, useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";
import { ArrowRightLeft, ChevronLeft, ChevronRight, Cpu, House, LayoutDashboard, ListChecks, Radio, Route, Router, Search } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { BytesLine, Panel, SharePie } from "@/components/charts";
import { RouteMap } from "@/components/route-map";
import { ProxyMap } from "@/components/proxy-map";
import { SystemDesign } from "@/components/system-design";
import { GatewayPanel } from "@/components/gateway";
import { AdminPage } from "@/components/admin";
import { Chat } from "@/components/chat";

type Metrics = {
  beforeLookups: number;
  beforeBytes: number;
  everySeconds: number;
  afterLookups: number;
  afterBytes: number;
  dropped: boolean;
};

type Card = {
  id: string;
  deviceId: string;
  deviceName: string;
  room: string;
  kind: string;
  addr?: string;
  mac?: string;
  destination: string;
  port?: number;
  category: string;
  protocol?: string;
  activity: string;
  sentence: string;
  consequence: string;
  suggestion: string;
  status: string;
  action: string;
  reroute: string;
  evidence: string;
  source?: string;
  llmAction?: string;
  llmReason?: string;
  selfSigned?: boolean;
  observations: number;
  metrics: Metrics;
};

type Device = { id: string; name: string; room: string; kind: string; addr?: string; mac?: string; viaAno?: boolean };
type ViaPath = { addr: string; port: number; proxy: boolean; dns: boolean; dnsDetail: string; steer?: boolean; steerDetail?: string };
type Decision = { at: string; deviceName: string; destination: string; outcome: string; detail: string };
type Sample = {
  at: string;
  patternId: string;
  destination: string;
  deviceName: string;
  leftBytes: number;
  keptBytes: number;
  lookups: number;
  refused: number;
};
type Envoy = { running: boolean; admin: string; listen: string; routes: number; message: string };
type LiveConn = { addr: string; destination: string; protocol: string; bytes: number };
export type AgentStatus = {
  name: string;
  mode: "passive" | "active";
  collecting: boolean;
  reviewed: number;
  detail: string;
  model: { kind: string; name: string; url: string; ready: boolean; detail: string };
};
export type DashboardState = {
  settings: { speakerOnly: boolean; mode?: "passive" | "active"; drops?: boolean };
  cards: Card[];
  devices: Device[];
  decisions: Decision[];
  series: Sample[];
  live?: LiveConn[];
  envoy: Envoy;
  agent?: AgentStatus;
  via?: ViaPath;
  addr?: string;
};

type View =
  | { kind: "overview" }
  | { kind: "records" }
  | { kind: "route" }
  | { kind: "system" }
  | { kind: "devices" }
  | { kind: "admin" }
  | { kind: "device"; id: string }
  | { kind: "pattern"; id: string }
  | { kind: "name"; name: string }
  | { kind: "decisions" }
  | { kind: "envoy" }
  | { kind: "proxy" };

const names: Record<string, string> = {
  "device-metrics-us.amazon.com": "Metrics US",
  "device-metrics-us-2.amazon.com": "Metrics US 2",
  "avs-alexa-na.amazon.com": "Voice answers",
  "app-measurement.com": "Ad measurement",
};

function shortName(card: Pick<Card, "destination" | "activity">) {
  return names[card.destination] ?? card.activity;
}

const servicePort: Record<string, number> = { AirPlay: 7000, Sonos: 1400, HTTPS: 443, HTTP: 80 };

function leavingTo(card: Pick<Card, "destination" | "port" | "addr">) {
  const dest = card.destination || "";
  const recorded = card.port && card.port > 0 ? String(card.port) : "";
  const port = recorded || (servicePort[dest] ? String(servicePort[dest]) : dest === "Cast" ? "8008/8009" : "");
  if (dest.includes(":")) return dest;
  if (dest.includes(".")) return port ? `${dest}:${port}` : dest;
  if (card.addr && port) return `${card.addr}:${port}`;
  if (port) return `${dest}:${port}`;
  return dest;
}

function visibleListen(value: string) {
  if (!value || value.startsWith("127.0.0.1:") || value.startsWith("[::1]:")) return "Ano";
  return value;
}

function suggestedLabel(category: string, suggestion: string) {
  if (category === "stream") return "Stays direct";
  if (suggestion === "drop") return "Drop";
  if (suggestion === "allow_direct" || suggestion === "allow") return "Allow";
  return "Not yet";
}

function bytes(n: number) {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(2)} GB`;
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)} MB`;
  if (n >= 1000) return `${(n / 1000).toFixed(1)} KB`;
  return `${n} B`;
}

function matchesOpen(conn: LiveConn, destination: string, addr?: string) {
  return conn.destination === destination && (!addr || conn.addr === addr);
}

function openOf(live: LiveConn[], destination: string, addr?: string) {
  const rows = live.filter((conn) => matchesOpen(conn, destination, addr));
  return { bytes: rows.reduce((sum, conn) => sum + conn.bytes, 0), count: rows.length };
}

function deviceOpen(live: LiveConn[], addr?: string) {
  if (!addr) return { bytes: 0, names: [] as string[] };
  const rows = live.filter((conn) => conn.addr === addr);
  return { bytes: rows.reduce((sum, conn) => sum + conn.bytes, 0), names: [...new Set(rows.map((conn) => conn.destination))] };
}

const pacific = { timeZone: "America/Los_Angeles" } as const;

function when(at: string) {
  return new Date(at).toLocaleString("en-US", { ...pacific, month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function hourLabel(at: string) {
  return new Date(at).toLocaleString("en-US", { ...pacific, month: "short", day: "numeric", hour: "numeric" });
}

function historyRows(samples: Sample[]) {
  const rows = new Map<string, { at: string; time: string; left: number; kept: number; lookups: number; refused: number }>();
  for (const sample of samples) {
    const row = rows.get(sample.at) ?? { at: sample.at, time: hourLabel(sample.at), left: 0, kept: 0, lookups: 0, refused: 0 };
    row.left += sample.leftBytes;
    row.kept += sample.keptBytes;
    row.lookups += sample.lookups;
    row.refused += sample.refused;
    rows.set(sample.at, row);
  }
  return [...rows.values()];
}

function useHistory(query: string) {
  const [rows, setRows] = useState<Sample[]>([]);
  useEffect(() => {
    if (!query) {
      setRows([]);
      return;
    }
    let stop = false;
    const pull = () => {
      fetch(`/api/history?${query}`, { cache: "no-store" })
        .then((response) => (response.ok ? response.json() : Promise.reject(new Error("history"))))
        .then((body: Sample[]) => {
          if (!stop) setRows(body ?? []);
        })
        .catch(() => {});
    };
    pull();
    const timer = setInterval(pull, 5000);
    return () => {
      stop = true;
      clearInterval(timer);
    };
  }, [query]);
  return rows;
}

function HistoryPanel({ samples, hint, fit = false }: { samples: Sample[]; hint: string; fit?: boolean }) {
  const [open, setOpen] = useState(false);
  const rows = historyRows(samples);
  const cutoff = Date.now() - 24 * 60 * 60 * 1000;
  const day = samples.filter((sample) => new Date(sample.at).getTime() >= cutoff);
  const left = day.reduce((sum, sample) => sum + sample.leftBytes, 0);
  const kept = day.reduce((sum, sample) => sum + sample.keptBytes, 0);
  const sends = day.reduce((sum, sample) => sum + sample.lookups, 0);
  const first = samples[0];
  const last = samples[samples.length - 1];
  const hours = [...rows].reverse();
  return (
    <Panel
      title="Bytes that left"
      hint={hint}
      className={fit ? "flex h-full min-h-0 flex-col" : ""}
      bodyClassName={fit ? "flex min-h-0 flex-1 flex-col p-3" : "p-4"}
    >
      {fit ? null : (
        <p className="mb-3 text-[13px] leading-5 text-muted-foreground">
          {samples.length === 0
            ? "No hours recorded for this name yet."
            : `${bytes(left)} left across ${sends} send${sends === 1 ? "" : "s"} in the last 24 hours. ${bytes(kept)} kept after a drop. First hour ${hourLabel(first.at)}. Latest hour ${hourLabel(last.at)}.`}
        </p>
      )}
      <div className={fit ? "relative min-h-0 flex-1" : undefined}>
        <div className={fit ? "absolute inset-0" : undefined}>
        <BytesLine
          className={fit ? "h-full w-full" : undefined}
          rows={rows}
          lines={[
            { key: "left", name: "Bytes that left", color: "#FF9830" },
            { key: "kept", name: "Bytes kept", color: "#73BF69" },
          ]}
        />
        </div>
      </div>
      {hours.length > 0 ? (
        <div className={open ? "relative z-20 shrink-0" : "relative shrink-0"}>
          <button type="button" onClick={() => setOpen(true)} className="mt-2 text-[13px] font-medium text-primary hover:underline">
            Results
          </button>
          {open ? <HoursDialog rows={hours} onClose={() => setOpen(false)} /> : null}
        </div>
      ) : null}
    </Panel>
  );
}

function HoursDialog({
  rows,
  onClose,
}: {
  rows: { at: string; time: string; left: number; kept: number; lookups: number }[];
  onClose: () => void;
}) {
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  return (
    <div className="absolute top-full right-0 left-0 z-30 mt-2 flex max-h-80 flex-col overflow-hidden rounded-xl border bg-card shadow-lg" role="dialog" aria-label="Bytes that left">
      <div className="flex items-center justify-between gap-3 border-b px-4 py-3">
        <p className="text-sm font-medium">
          Bytes that left
          <span className="ml-2 text-[12px] font-normal text-muted-foreground">{rows.length} hours</span>
        </p>
        <button type="button" onClick={onClose} className="text-[13px] font-medium text-primary hover:underline">
          Close
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-4">
        <table className="w-full text-left text-sm">
          <thead className="sticky top-0 bg-card text-[11px] tracking-wide text-muted-foreground uppercase">
            <tr>
              <th className="py-2 pr-3 font-medium">Hour</th>
              <th className="py-2 pr-3 font-medium">Left</th>
              <th className="py-2 pr-3 font-medium">Sends</th>
              <th className="py-2 font-medium">Kept</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.at} className="border-t">
                <td className="py-2 pr-3">{row.time}</td>
                <td className="py-2 pr-3 tabular-nums">{bytes(row.left)}</td>
                <td className="py-2 pr-3 tabular-nums">{row.lookups}</td>
                <td className="py-2 tabular-nums">{bytes(row.kept)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

export function Home({ initialState = null }: { initialState?: DashboardState | null }) {
  const [state, setState] = useState<DashboardState | null>(initialState);
  const [error, setError] = useState("");
  const [view, setView] = useState<View>({ kind: "overview" });
  const [host, setHost] = useState("device-metrics-us.amazon.com");
  const [probe, setProbe] = useState("");

  async function load() {
    const response = await fetch("/api/state?all=1", { cache: "no-store" });
    if (!response.ok) throw new Error("Ano did not answer");
    const body = (await response.json()) as DashboardState;
    setState({ ...body, series: body.series ?? [], cards: body.cards ?? [], devices: body.devices ?? [], decisions: body.decisions ?? [], live: body.live ?? [] });
    setError("");
  }

  useEffect(() => {
    const pull = () => {
      load().catch((err: Error) => setError(err.message));
    };
    pull();
    const timer = setInterval(pull, 2000);
    return () => clearInterval(timer);
  }, []);

  async function decide(id: string, action: "drop" | "allow_direct" | "allow_envoy" | "ignore") {
    const response = await fetch(`/api/cards/${id}/decide`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action }),
    });
    if (!response.ok) {
      setError("That choice did not save");
      return;
    }
    await load();
  }

  async function decideName(name: string, action: "drop" | "allow_direct") {
    const response = await fetch("/api/names/decide", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, action }),
    });
    if (!response.ok) {
      setError("That choice did not save");
      return;
    }
    await load();
  }

  function openName(name: string) {
    setView({ kind: "name", name });
  }

  async function setSpeakerOnly(speakerOnly: boolean) {
    await fetch("/api/settings", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ speakerOnly }),
    });
    setView({ kind: "overview" });
    await load();
  }

  async function setDrops(on: boolean) {
    const response = await fetch("/api/settings", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ drops: on }),
    });
    if (!response.ok) {
      setError("Ano could not change drops");
      return;
    }
    await load();
  }

  async function setMode(mode: "passive" | "active") {
    const response = await fetch("/api/settings", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ mode }),
    });
    if (!response.ok) {
      setError("Ano could not change mode");
      return;
    }
    await load();
  }

  async function setVia(id: string, on: boolean) {
    const response = await fetch(`/api/devices/${id}/via`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ on }),
    });
    if (!response.ok) {
      setError("Ano could not add that device");
      return;
    }
    await load();
  }

  async function setSteer(on: boolean) {
    const response = await fetch("/api/admin/steer", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ on }),
    });
    const body = (await response.json()) as { error?: string };
    if (!response.ok) {
      setError(body.error || "Ano could not take the Wi-Fi path");
      return;
    }
    setError("");
    await load();
  }

  async function runProbe(event: FormEvent) {
    event.preventDefault();
    setProbe("Checking…");
    const response = await fetch("/api/probe", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ host }),
    });
    const body = (await response.json()) as { status?: number; body?: string; error?: string };
    setProbe(body.error ? body.error : `HTTP ${body.status}: ${body.body}`);
  }

  if (!state) {
    return (
      <main className="grid min-h-screen place-items-center bg-background px-6 text-center text-foreground">
        <div className="max-w-sm">
          <p className="text-sm text-muted-foreground">{error || "Loading the dashboard…"}</p>
          {error ? (
            <button type="button" className="mt-3 text-sm text-primary" onClick={() => load().catch((err: Error) => setError(err.message))}>
              Try again
            </button>
          ) : null}
        </div>
      </main>
    );
  }

  const devices = state.devices ?? [];
  const selected = view.kind === "pattern" ? (state.cards ?? []).find((card) => card.id === view.id) : undefined;
  const device = view.kind === "device" ? devices.find((item) => item.id === view.id) : undefined;

  return (
    <div className="flex h-dvh justify-center bg-[oklch(0.93_0.012_260)] p-3 text-foreground md:p-5">
      <div className="flex min-h-0 w-full max-w-[1440px] overflow-hidden rounded-2xl border bg-background shadow-[0_16px_48px_rgba(16,24,40,0.1)]">
      <aside className="flex w-16 shrink-0 flex-col border-r bg-sidebar md:w-56">
        <div className="flex items-center gap-2.5 px-3 py-5 md:px-4">
          <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-primary text-[13px] font-semibold text-primary-foreground">A</span>
          <div className="hidden min-w-0 md:block">
            <p className="text-sm font-semibold tracking-tight">Ano</p>
            <p className="text-[11px] text-muted-foreground">Secured Home</p>
            {state.addr ? <p className="truncate font-mono text-[11px] text-muted-foreground">{state.addr}</p> : null}
          </div>
        </div>
        <nav className="flex-1 space-y-0.5 overflow-y-auto px-2 pb-3 md:px-3">
          <NavButton icon={LayoutDashboard} active={view.kind === "overview"} onClick={() => setView({ kind: "overview" })}>
            Overview
          </NavButton>
          <NavButton icon={Search} active={view.kind === "records"} onClick={() => setView({ kind: "records" })}>
            Records
          </NavButton>
          <NavButton icon={Route} active={view.kind === "route"} onClick={() => setView({ kind: "route" })}>
            Route
          </NavButton>
          <NavButton icon={Cpu} active={view.kind === "system"} onClick={() => setView({ kind: "system" })}>
            System
          </NavButton>
          <NavButton icon={House} active={view.kind === "devices" || view.kind === "device" || view.kind === "pattern"} onClick={() => setView({ kind: "devices" })}>
            Devices
          </NavButton>
          <NavButton icon={Router} active={view.kind === "admin"} onClick={() => setView({ kind: "admin" })}>
            Admin
          </NavButton>
          <NavButton icon={ListChecks} active={view.kind === "decisions"} onClick={() => setView({ kind: "decisions" })}>
            Decisions
          </NavButton>
          <NavButton icon={Radio} active={view.kind === "envoy"} onClick={() => setView({ kind: "envoy" })}>
            Envoy
          </NavButton>
          <NavButton icon={ArrowRightLeft} active={view.kind === "proxy"} onClick={() => setView({ kind: "proxy" })}>
            Proxy
          </NavButton>
        </nav>
        <div className="border-t px-2 py-4 md:px-4">
          <button
            type="button"
            aria-pressed={state.settings.drops !== false}
            aria-label="Drops"
            onClick={() => setDrops(state.settings.drops === false)}
            className={`mb-2 hidden h-8 w-full rounded-md border text-[12px] font-medium md:block ${state.settings.drops !== false ? "border-primary bg-primary text-primary-foreground" : "bg-card text-muted-foreground"}`}
          >
            {state.settings.drops === false ? "Drops off" : "Drops on"}
          </button>
          <p className="mb-3 hidden text-[11px] leading-4 text-muted-foreground md:block">
            {state.settings.drops === false ? "Traffic goes through. Saved choices stay put." : "Each drop and allow is in effect."}
          </p>
          <div className="mb-3 hidden gap-1 md:flex" role="group" aria-label="Agent mode">
            <ModeButton active={(state.agent?.mode ?? state.settings.mode ?? "passive") === "passive"} onClick={() => setMode("passive")}>
              Passive
            </ModeButton>
            <ModeButton active={(state.agent?.mode ?? "passive") === "active"} onClick={() => setMode("active")}>
              Active
            </ModeButton>
          </div>
          <p className="mb-3 hidden text-[11px] leading-4 text-muted-foreground md:block">
            {state.agent?.model.ready ? "AI Inference" : "AI Inference offline"} · {state.agent?.mode ?? "passive"}
          </p>
          <label className="flex items-center justify-center gap-2 text-[13px] text-foreground md:justify-start">
            <input
              type="checkbox"
              aria-label="Speakers only"
              className="size-3.5 accent-[oklch(0.46_0.14_264)]"
              checked={state.settings.speakerOnly}
              onChange={(event) => setSpeakerOnly(event.target.checked)}
            />
            <span className="hidden md:inline">Speakers only</span>
          </label>
        </div>
      </aside>

      <main className={`min-h-0 min-w-0 flex-1 px-5 py-5 md:px-8 md:py-6 ${view.kind === "device" || view.kind === "records" ? "flex flex-col overflow-hidden" : "overflow-y-auto"}`}>
          {error ? <p className="mb-4 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm">{error}</p> : null}
          {view.kind === "overview" ? (
            <Overview
              cards={state.cards ?? []}
              pathCards={state.cards ?? []}
              decisions={state.decisions}
              live={state.live ?? []}
              envoy={state.envoy}
              open={(id) => setView({ kind: "pattern", id })}
              onDecide={decide}
              onDecideName={decideName}
              onOpenName={openName}
              showRoute={() => setView({ kind: "route" })}
            />
          ) : null}
          {view.kind === "records" ? (
            <RecordsPage cards={state.cards ?? []} live={state.live ?? []} open={(id) => setView({ kind: "pattern", id })} onDecide={decide} onDecideName={decideName} onOpenName={openName} />
          ) : null}
          {view.kind === "route" ? (
            <RoutePage cards={state.cards} live={state.live ?? []} open={(id) => setView({ kind: "pattern", id })} onDecide={decide} onDecideName={decideName} onOpenName={openName} />
          ) : null}
          {view.kind === "system" ? (
            <div className="space-y-8">
              <GatewayPanel />
              <SystemDesign agent={state.agent} />
            </div>
          ) : null}
          {view.kind === "admin" ? <AdminPage open={(id) => setView({ kind: "device", id })} /> : null}
          {view.kind === "devices" ? (
            <DevicesPage devices={devices} cards={state.cards ?? []} live={state.live ?? []} speakerOnly={state.settings.speakerOnly} open={(id) => setView({ kind: "device", id })} />
          ) : null}
          {view.kind === "device" && device ? (
            <DeviceView
              device={device}
              cards={state.cards.filter((card) => card.deviceId === device.id)}
              live={state.live ?? []}
              via={state.via}
              open={(id) => setView({ kind: "pattern", id })}
              onDecide={decide}
              onDecideName={decideName}
              onOpenName={openName}
              onBack={() => setView({ kind: "devices" })}
              onVia={setVia}
              onSteer={setSteer}
            />
          ) : null}
          {view.kind === "name" ? (
            <NameView
              name={view.name}
              cards={(state.cards ?? []).filter((card) => shortName(card).toLowerCase() === view.name.toLowerCase())}
              live={state.live ?? []}
              open={(id) => setView({ kind: "pattern", id })}
              onDecideName={decideName}
              onBack={() => setView({ kind: "records" })}
            />
          ) : null}
          {view.kind === "pattern" && selected ? (
            <PatternView
              card={selected}
              live={state.live ?? []}
              onDecide={decide}
              onDevices={() => setView({ kind: "devices" })}
              onDevice={() => setView({ kind: "device", id: selected.deviceId })}
            />
          ) : null}
          {view.kind === "decisions" ? <Decisions decisions={state.decisions} /> : null}
          {view.kind === "proxy" ? <ProxyMap /> : null}
          {view.kind === "envoy" ? (
            <EnvoyView
              envoy={state.envoy}
              cards={state.cards ?? []}
              live={state.live ?? []}
              host={host}
              setHost={setHost}
              probe={probe}
              onProbe={runProbe}
              open={(id) => setView({ kind: "pattern", id })}
              onDecide={decide}
              onDecideName={decideName}
              onOpenName={openName}
            />
          ) : null}
        </main>
        <Chat onChanged={() => load().catch((err: Error) => setError(err.message))} />
      </div>
    </div>
  );
}

function ModeButton({ active, onClick, children }: { active: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={`h-7 flex-1 rounded-md border text-[12px] ${active ? "border-primary bg-primary text-primary-foreground" : "bg-card text-muted-foreground"}`}
    >
      {children}
    </button>
  );
}

function NavButton({
  active,
  icon: Icon,
  children,
  onClick,
}: {
  active: boolean;
  icon: LucideIcon;
  children: ReactNode;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      title={typeof children === "string" ? children : undefined}
      onClick={onClick}
      className={`flex w-full items-center justify-center gap-2.5 rounded-lg px-2.5 py-2 text-left text-[13px] md:justify-start ${
        active ? "bg-accent font-medium text-accent-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground"
      }`}
    >
      <Icon className="size-4 shrink-0" strokeWidth={1.75} />
      <span className="hidden md:inline">{children}</span>
    </button>
  );
}

function Stat({ label, value, note, tone = "#98A2B3" }: { label: string; value: string; note: string; tone?: string }) {
  return (
    <div className="rounded-xl border bg-card px-4 py-3.5 shadow-[0_1px_2px_rgba(16,24,40,0.04)]">
      <div className="flex items-center gap-2">
        <span className="size-1.5 rounded-full" style={{ background: tone }} />
        <p className="text-[12px] text-muted-foreground">{label}</p>
      </div>
      <p className={`mt-2 font-semibold tracking-tight tabular-nums leading-none ${value.length > 14 ? "text-base" : "text-[26px]"}`}>{value}</p>
      <p className="mt-2 text-[12px] leading-4 text-muted-foreground">{note}</p>
    </div>
  );
}

function StatusPill({ card }: { card: Card }) {
  const label = card.metrics.dropped ? "Dropped" : card.status === "approved" ? "Allowed" : "Needs a decision";
  const tone = card.metrics.dropped
    ? "bg-[#F2495C]/10 text-[#C43143]"
    : card.status === "approved"
      ? "bg-[#73BF69]/15 text-[#2F7A32]"
      : "bg-[#FF9830]/15 text-[#B86A12]";
  return <span className={`inline-flex rounded-full px-2 py-0.5 text-[11px] font-medium ${tone}`}>{label}</span>;
}

function Overview({
  cards,
  pathCards,
  decisions,
  live,
  envoy,
  open,
  onDecide,
  onDecideName,
  onOpenName,
  showRoute,
}: {
  cards: Card[];
  pathCards: Card[];
  decisions: Decision[];
  live: LiveConn[];
  envoy: Envoy;
  open: (id: string) => void;
  onDecide: (id: string, action: Choice) => void;
  onDecideName: (name: string, action: "drop" | "allow_direct") => void;
  onOpenName: (name: string) => void;
  showRoute: () => void;
}) {
  const endpoints = useMemo(() => {
    const seen = new Map<string, { label: string; selfSigned: boolean }>();
    for (const card of cards) {
      if (!card.destination) continue;
      const label = shortName(card);
      const shown = label && label !== card.destination ? `${label} · ${card.destination}` : card.destination;
      const prev = seen.get(card.destination);
      if (!prev) {
        seen.set(card.destination, { label: shown, selfSigned: Boolean(card.selfSigned) });
        continue;
      }
      if (card.selfSigned) prev.selfSigned = true;
    }
    for (const conn of live) {
      if (conn.destination && !seen.has(conn.destination)) seen.set(conn.destination, { label: conn.destination, selfSigned: false });
    }
    return [...seen.entries()]
      .map(([value, item]) => ({ value, label: item.label, selfSigned: item.selfSigned }))
      .sort((a, b) => a.label.localeCompare(b.label));
  }, [cards, live]);
  const openNames = useMemo(() => new Set(live.map((conn) => conn.destination)), [live]);
  const [picked, setPicked] = useState("");
  const selected = endpoints.some((item) => item.value === picked)
    ? picked
    : (endpoints.find((item) => openNames.has(item.value))?.value ?? endpoints[0]?.value ?? "");
  const shown = cards.filter((card) => card.destination === selected);
  const samples = useHistory(selected ? `destination=${encodeURIComponent(selected)}` : "");
  const left = shown.reduce((sum, card) => sum + card.metrics.beforeBytes, 0);
  const kept = shown.reduce((sum, card) => sum + card.metrics.afterBytes, 0);
  const refused = shown.reduce((sum, card) => sum + card.metrics.afterLookups, 0);
  const now = openOf(live, selected);
  const routeCards = pathCards.filter((card) => card.destination === selected);
  const chosenDecisions = decisions.filter((item) => item.destination === selected);
  const openEndpoints = endpoints.filter((item) => !item.selfSigned && openNames.has(item.value));
  const selfSignedEndpoints = endpoints.filter((item) => item.selfSigned);
  const collectedEndpoints = endpoints.filter((item) => !item.selfSigned && !openNames.has(item.value));
  const selectedSelfSigned = endpoints.some((item) => item.value === selected && item.selfSigned);
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[22px] font-semibold tracking-tight">Overview</h1>
          <p className="mt-1 max-w-2xl text-[13px] leading-5 text-muted-foreground">
            Open now is a connection that is up. Collected is everything Ano has recorded, including connections that already closed.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <label className="flex items-center gap-2 text-[13px] text-muted-foreground">
            Endpoint
            <select
              aria-label="Endpoint"
              value={selected}
              onChange={(event) => setPicked(event.target.value)}
              disabled={endpoints.length === 0}
              className="h-9 max-w-[280px] rounded-md border bg-background px-2 text-sm text-foreground"
            >
              {endpoints.length === 0 ? <option value="">No names yet</option> : null}
              {openEndpoints.length > 0 ? (
                <optgroup label="Open now">
                  {openEndpoints.map((item) => (
                    <option key={item.value} value={item.value}>
                      {item.label}
                    </option>
                  ))}
                </optgroup>
              ) : null}
              {selfSignedEndpoints.length > 0 ? (
                <optgroup label="Self-signed">
                  {selfSignedEndpoints.map((item) => (
                    <option key={item.value} value={item.value}>
                      {item.label}
                    </option>
                  ))}
                </optgroup>
              ) : null}
              {collectedEndpoints.length > 0 ? (
                <optgroup label="Collected">
                  {collectedEndpoints.map((item) => (
                    <option key={item.value} value={item.value}>
                      {item.label}
                    </option>
                  ))}
                </optgroup>
              ) : null}
            </select>
          </label>
          <span className="rounded-full border bg-card px-2.5 py-1 text-[12px] text-muted-foreground">
            {envoy.running ? "Live" : "Envoy down"}
          </span>
        </div>
      </div>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Stat label="Open now" value={bytes(now.bytes)} note={now.count === 0 ? "No open connection" : `${now.count} open connection${now.count === 1 ? "" : "s"}`} tone="#5794F2" />
        <Stat label="Collected" value={bytes(left)} note="Recorded so far, including closed connections" tone="#FF9830" />
        <Stat label="Bytes kept" value={bytes(kept)} note="Refused at Ano after a drop" tone="#73BF69" />
        <Stat label="Lookups refused" value={String(refused)} note="Collected speaker checks that did not go out" tone="#F2495C" />
      </div>
      <HistoryPanel samples={samples} hint="Each hour this name went out. Use the hours to choose drop or allow." />
      <div className="grid gap-4 xl:grid-cols-2">
        <Panel title="Collected bytes that left" hint="Share of recorded bytes, by name">
          <SharePie slices={shown.map((card) => ({ name: shortName(card), value: card.metrics.beforeBytes }))} />
        </Panel>
        <Panel title="What Ano kept" hint="Share of refused bytes, by name">
          <SharePie slices={shown.map((card) => ({ name: shortName(card), value: card.metrics.afterBytes }))} />
        </Panel>
      </div>
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.4fr)_minmax(280px,0.8fr)]">
        <Panel title="This name" hint={selectedSelfSigned ? "Self-signed certificate" : now.count === 0 ? "Collected only. Nothing is open." : "Open now, and the collected total"}>
          <div className="overflow-x-auto">
            <table className="w-full min-w-[640px] text-left text-sm">
              <thead className="text-[11px] tracking-wide text-muted-foreground uppercase">
                <tr>
                  <th className="pb-2 pr-3 font-medium">Name</th>
                  <th className="pb-2 pr-3 font-medium">Device</th>
                  <th className="pb-2 pr-3 font-medium">Open now</th>
                  <th className="pb-2 pr-3 font-medium">Collected</th>
                  <th className="pb-2 pr-3 font-medium">Kept</th>
                  <th className="pb-2 font-medium">Now</th>
                </tr>
              </thead>
              <tbody>
                {shown.length === 0 && now.count > 0 ? (
                  <tr className="border-t">
                    <td className="py-3 pr-3 font-mono text-xs">{selected}</td>
                    <td className="py-3 pr-3 text-muted-foreground">Seen just now</td>
                    <td className="py-3 pr-3 tabular-nums">{bytes(now.bytes)}</td>
                    <td className="py-3 pr-3 tabular-nums">0 B</td>
                    <td className="py-3 pr-3 tabular-nums">0 B</td>
                    <td className="py-3 text-[12px] text-[#1F6FEB]">Open</td>
                  </tr>
                ) : null}
                {shown.map((card) => {
                  const row = openOf(live, card.destination, card.addr);
                  return (
                    <tr key={card.id} className="border-t">
                      <td className="py-3 pr-3">
                        <button type="button" className="inline-flex items-center gap-1 text-left font-medium text-primary hover:underline" onClick={() => open(card.id)}>
                          {shortName(card)}
                          <ChevronRight className="size-3.5" />
                          <span className="sr-only">Open this name</span>
                        </button>
                        <p className="font-mono text-xs text-muted-foreground">{leavingTo(card)}</p>
                      </td>
                      <td className="py-3 pr-3">{card.deviceName}</td>
                      <td className="py-3 pr-3 tabular-nums">{row.count === 0 ? "None" : bytes(row.bytes)}</td>
                      <td className="py-3 pr-3 tabular-nums">{bytes(card.metrics.beforeBytes)}</td>
                      <td className="py-3 pr-3 tabular-nums">{bytes(card.metrics.afterBytes)}</td>
                      <td className="py-3">
                        <StatusPill card={card} />
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </Panel>
        <Panel title="Latest decisions" hint="Collected choices, not open connections">
          <DecisionList decisions={chosenDecisions.slice(0, 6)} />
        </Panel>
      </div>
      <Panel title="Path out of the house" hint={now.count === 0 ? "Collected path. Nothing on this name is open." : "Open now on this name"}>
        <RouteMap cards={routeCards} onOpen={open} compact />
        <button type="button" onClick={showRoute} className="mt-3 text-[13px] text-primary hover:underline">
          Open the full route
        </button>
      </Panel>
      <RecordTabs cards={cards} live={live} open={open} onDecide={onDecide} onDecideName={onDecideName} onOpenName={onOpenName} />
    </div>
  );
}

function RoutePage({
  cards,
  live,
  open,
  onDecide,
  onDecideName,
  onOpenName,
}: {
  cards: Card[];
  live: LiveConn[];
  open: (id: string) => void;
  onDecide: (id: string, action: Choice) => void;
  onDecideName: (name: string, action: "drop" | "allow_direct") => void;
  onOpenName: (name: string) => void;
}) {
  const openCards = cards.filter((card) => openOf(live, card.destination, card.addr).count > 0);
  const collected = cards.filter((card) => openOf(live, card.destination, card.addr).count === 0);
  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Route</h1>
        <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">
          Open now is a connection that is up. Collected is a name Ano already recorded.
        </p>
      </div>
      <section className="space-y-3">
        <h2 className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">Open now</h2>
        {openCards.length === 0 ? <p className="text-sm text-muted-foreground">No open connection.</p> : <RouteMap cards={openCards} onOpen={open} />}
      </section>
      <section className="space-y-3">
        <h2 className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">Collected</h2>
        {collected.length === 0 ? <p className="text-sm text-muted-foreground">Nothing collected yet.</p> : <RouteMap cards={collected} onOpen={open} />}
      </section>
      <RecordTabs cards={cards} live={live} open={open} onDecide={onDecide} onDecideName={onDecideName} onOpenName={onOpenName} />
    </div>
  );
}

function DevicesPage({
  devices,
  cards,
  live,
  speakerOnly,
  open,
}: {
  devices: Device[];
  cards: Card[];
  live: LiveConn[];
  speakerOnly: boolean;
  open: (id: string) => void;
}) {
  const order = ["speaker", "tv", "thermostat", "phone"];
  const kinds = [...new Set(devices.map((device) => device.kind))].sort((a, b) => {
    const ai = order.indexOf(a);
    const bi = order.indexOf(b);
    return (ai === -1 ? 99 : ai) - (bi === -1 ? 99 : bi);
  });
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Devices</h1>
        <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">
          Open a device, then a name, for its charts.
        </p>
      </div>
      {kinds.length === 0 ? <p className="text-sm text-muted-foreground">No devices yet.</p> : null}
      {kinds.map((kind) => (
        <section key={kind}>
          <h2 className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">{kindTitle(kind)}</h2>
          <ul className="mt-2 grid gap-2 sm:grid-cols-2">
            {devices
              .filter((device) => device.kind === kind)
              .map((device) => {
                const own = cards.filter((card) => card.deviceId === device.id);
                const left = own.reduce((sum, card) => sum + card.metrics.beforeBytes, 0);
                const kept = own.reduce((sum, card) => sum + card.metrics.afterBytes, 0);
                const now = deviceOpen(live, device.addr);
                return (
                  <li key={device.id}>
                    <button
                      type="button"
                      onClick={() => open(device.id)}
                      className="flex w-full items-center gap-3 rounded-xl border bg-card px-4 py-3.5 text-left shadow-[0_1px_2px_rgba(16,24,40,0.04)] hover:border-primary hover:bg-accent/50"
                    >
                      <span className="min-w-0 flex-1">
                        <span className="flex items-baseline justify-between gap-3">
                          <span className="text-sm font-medium">{device.name}</span>
                          <span className="text-[11px] text-muted-foreground">{device.room}</span>
                        </span>
                        <span className="mt-1 block text-[12px] text-muted-foreground">{deviceIdentity({ kind: device.kind, addr: device.addr, mac: device.mac })}</span>
                        <span className="mt-1 block text-[12px] text-muted-foreground">
                          {device.viaAno ? "Through Ano · " : ""}
                          {own.length === 0 && now.names.length === 0
                            ? "No names yet"
                            : `Open now ${bytes(now.bytes)} · collected ${bytes(left)} · kept ${bytes(kept)}`}
                        </span>
                        {now.names.length > 0 ? (
                          <span className="mt-1 block truncate font-mono text-[11px] text-[#1F6FEB]">Open now {now.names.join(", ")}</span>
                        ) : (
                          <span className="mt-1 block text-[11px] text-muted-foreground">No open connection</span>
                        )}
                        {speakerOnly && device.kind !== "speaker" ? (
                          <span className="mt-1 block text-[11px] text-muted-foreground">Overview shows one name at a time. Open still shows every name.</span>
                        ) : null}
                      </span>
                      <OpenChip label="Open" />
                    </button>
                  </li>
                );
              })}
          </ul>
        </section>
      ))}
    </div>
  );
}

function deviceIdentity(device: { room?: string; kind: string; addr?: string; mac?: string }) {
  return [device.room, singularKind(device.kind), device.addr, device.mac].filter((part) => part && part.trim() !== "").join(" · ");
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
  return kind;
}

function kindTitle(kind: string) {
  if (kind === "speaker") return "Speakers";
  if (kind === "phone") return "Phones";
  if (kind === "tv") return "TVs";
  if (kind === "camera") return "Cameras";
  if (kind === "garage") return "Garage";
  if (kind === "light") return "Lights";
  if (kind === "printer") return "Printers";
  if (kind === "thermostat") return "Thermostats";
  if (kind === "computer") return "Computers";
  if (kind === "unknown") return "Other devices";
  return kind;
}

function shareSlices(cards: Card[]) {
  const totals = new Map<string, number>();
  for (const card of cards) {
    if (card.metrics.beforeBytes <= 0) continue;
    const name = shortName(card) || card.destination;
    totals.set(name, (totals.get(name) ?? 0) + card.metrics.beforeBytes);
  }
  const ranked = [...totals.entries()].sort((a, b) => b[1] - a[1]);
  const top = ranked.slice(0, 6).map(([name, value]) => ({ name, value }));
  const other = ranked.slice(6).reduce((sum, [, value]) => sum + value, 0);
  if (other > 0) top.push({ name: "Other", value: other });
  return top;
}

function devicePath(device: Device, via?: ViaPath) {
  if (!device.viaAno) {
    return "Ano records the conversations it can see with this device.";
  }
  if (via?.steer) {
    return "A redirect is still on. Return this device to the router.";
  }
  return "This device is added. Its own packets still go to the router. Ano leaves that redirect off.";
}

function OpenChip({ label }: { label: string }) {
  return (
    <span className="inline-flex shrink-0 items-center gap-0.5 rounded-md border border-primary/30 bg-primary/10 px-2 py-1 text-[12px] font-medium text-primary">
      {label}
      <ChevronRight className="size-3.5" />
    </span>
  );
}

const pageSize = 20;

type RecordRow = {
  key: string;
  name: string;
  dest: string;
  device: string;
  addr: string;
  at?: string;
  openBytes: number;
  openCount: number;
  collected: number;
  suggestion: string;
  category: string;
  action: string;
  status: string;
  selfSigned: boolean;
  reason: string;
  cardId?: string;
};

function addressName(name: string) {
  const host = name.trim().toLowerCase().replace(/:\d+$/, "");
  return /^\d{1,3}(\.\d{1,3}){3}$/.test(host);
}

function groupKey(row: RecordRow) {
  const name = row.name.trim();
  if (!name || name === row.dest || addressName(name)) return `one:${row.dest}|${row.addr}`;
  return name.toLowerCase();
}

type RecordGroup = {
  key: string;
  name: string;
  rows: RecordRow[];
  openBytes: number;
  openCount: number;
  collected: number;
  at?: string;
  device: string;
  suggestion: string;
  category: string;
  unanimous: boolean;
};

function groupRecords(rows: RecordRow[]): RecordGroup[] {
  const order: string[] = [];
  const map = new Map<string, RecordRow[]>();
  for (const row of rows) {
    const key = groupKey(row);
    const list = map.get(key);
    if (!list) {
      map.set(key, [row]);
      order.push(key);
    } else {
      list.push(row);
    }
  }
  return order.map((key) => {
    const list = (map.get(key) ?? []).slice().sort((a, b) => b.collected - a.collected || a.dest.localeCompare(b.dest));
    const devices = [...new Set(list.map((row) => row.device).filter(Boolean))];
    const labels = new Set(list.map((row) => suggestedLabel(row.category, row.suggestion)));
    const newest = list.map((row) => row.at ?? "").sort().at(-1) ?? "";
    return {
      key,
      name: list[0]?.name ?? "",
      rows: list,
      openBytes: list.reduce((sum, row) => sum + row.openBytes, 0),
      openCount: list.reduce((sum, row) => sum + row.openCount, 0),
      collected: list.reduce((sum, row) => sum + row.collected, 0),
      at: newest,
      device: devices.length <= 1 ? (devices[0] ?? "") : `${devices.length} devices`,
      suggestion: labels.size === 1 ? (list[0]?.suggestion ?? "") : "",
      category: labels.size === 1 ? (list[0]?.category ?? "") : "",
      unanimous: labels.size === 1,
    };
  });
}

function rowMatches(row: RecordRow, query: string, kind: string) {
  const q = query.trim().toLowerCase();
  if (q) {
    const kindText = [suggestedLabel(row.category, row.suggestion), row.selfSigned ? "self-signed" : "", row.reason].join(" ");
    const hay = `${row.name} ${row.dest} ${row.addr} ${row.device} ${kindText}`.toLowerCase();
    if (!hay.includes(q)) return false;
  }
  if (kind === "self-signed") return row.selfSigned;
  if (kind === "none") return row.reason.toLowerCase().includes("no certificate");
  if (kind === "drop") return row.suggestion === "drop" || (row.status === "approved" && row.action === "drop");
  if (kind === "allow") return row.suggestion === "allow" || row.suggestion === "allow_direct" || (row.status === "approved" && (row.action === "allow_direct" || row.action === "allow"));
  if (kind === "notyet") return suggestedLabel(row.category, row.suggestion) === "Not yet";
  return true;
}

function liveRecords(cards: Card[], live: LiveConn[]): RecordRow[] {
  const rows: RecordRow[] = [];
  const seen = new Set<string>();
  for (const card of cards) {
    const open = openOf(live, card.destination, card.addr);
    if (open.count === 0) continue;
    seen.add(`${card.addr ?? ""}|${card.destination}`);
    rows.push({
      key: card.id,
      name: shortName(card),
      dest: leavingTo(card),
      device: card.deviceName,
      addr: card.addr ?? "",
      openBytes: open.bytes,
      openCount: open.count,
      collected: card.metrics.beforeBytes,
      suggestion: card.suggestion,
      category: card.category,
      action: card.action,
      status: card.status,
      selfSigned: Boolean(card.selfSigned),
      reason: card.llmReason ?? "",
      cardId: card.id,
    });
  }
  for (const conn of live) {
    if (cards.some((card) => matchesOpen(conn, card.destination, card.addr))) continue;
    const card = cards.find((item) => item.destination === conn.destination);
    const key = `${conn.addr}|${conn.destination}|${conn.protocol}`;
    if (seen.has(key)) continue;
    seen.add(key);
    rows.push({
      key,
      name: card ? shortName(card) : conn.destination,
      dest: conn.destination,
      device: card?.deviceName || conn.addr,
      addr: conn.addr,
      openBytes: conn.bytes,
      openCount: 1,
      collected: card?.metrics.beforeBytes ?? 0,
      suggestion: card?.suggestion ?? "",
      category: card?.category ?? "",
      action: card?.action ?? "",
      status: card?.status ?? "",
      selfSigned: Boolean(card?.selfSigned),
      reason: card?.llmReason ?? "",
      cardId: card?.id,
    });
  }
  return rows.sort((a, b) => b.openBytes - a.openBytes || a.name.localeCompare(b.name));
}

function NameActions({
  name,
  rows,
  onDecideName,
  onOpenName,
}: {
  name: string;
  rows: RecordRow[];
  onDecideName?: (name: string, action: "drop" | "allow_direct") => void;
  onOpenName?: (name: string) => void;
}) {
  const dropped = rows.length > 0 && rows.every((row) => row.status === "approved" && row.action === "drop");
  const allowed = rows.length > 0 && rows.every((row) => row.status === "approved" && row.action === "allow_direct");
  const canChoose = rows.some((row) => row.cardId && row.category !== "stream");
  return (
    <>
      {canChoose && onDecideName ? (
        <span className="mr-2 inline-flex gap-1">
          <button
            type="button"
            aria-pressed={dropped}
            onClick={() => onDecideName(name, "drop")}
            className={`h-7 rounded-md border px-2 text-[12px] ${dropped ? "border-[#F2495C] bg-[#F2495C] text-white" : "bg-card text-foreground/80 hover:bg-muted"}`}
          >
            Drop
          </button>
          <button
            type="button"
            aria-pressed={allowed}
            onClick={() => onDecideName(name, "allow_direct")}
            className={`h-7 rounded-md border px-2 text-[12px] ${allowed ? "border-[#73BF69] bg-[#73BF69] text-[#10210f]" : "bg-card text-foreground/80 hover:bg-muted"}`}
          >
            Allow
          </button>
        </span>
      ) : null}
      <button
        type="button"
        disabled={!onOpenName}
        onClick={() => onOpenName?.(name)}
        className="inline-flex h-7 items-center gap-0.5 rounded-md border border-primary/30 bg-primary/10 px-2 text-[12px] font-medium text-primary disabled:opacity-40"
      >
        Open
        <ChevronRight className="size-3.5" />
      </button>
    </>
  );
}

function RecordActions({
  row,
  open,
  onDecide,
}: {
  row: RecordRow;
  open: (id: string) => void;
  onDecide?: (id: string, action: Choice) => void;
}) {
  return (
    <>
      {row.cardId && row.category !== "stream" && onDecide ? (
        <span className="mr-2 inline-flex gap-1">
          <button
            type="button"
            aria-pressed={row.status === "approved" && row.action === "drop"}
            onClick={() => onDecide(row.cardId!, "drop")}
            className={`h-7 rounded-md border px-2 text-[12px] ${row.status === "approved" && row.action === "drop" ? "border-[#F2495C] bg-[#F2495C] text-white" : "bg-card text-foreground/80 hover:bg-muted"}`}
          >
            Drop
          </button>
          <button
            type="button"
            aria-pressed={row.status === "approved" && row.action === "allow_direct"}
            onClick={() => onDecide(row.cardId!, "allow_direct")}
            className={`h-7 rounded-md border px-2 text-[12px] ${row.status === "approved" && row.action === "allow_direct" ? "border-[#73BF69] bg-[#73BF69] text-[#10210f]" : "bg-card text-foreground/80 hover:bg-muted"}`}
          >
            Allow
          </button>
        </span>
      ) : null}
      <button
        type="button"
        disabled={!row.cardId}
        onClick={() => row.cardId && open(row.cardId)}
        className="inline-flex h-7 items-center gap-0.5 rounded-md border border-primary/30 bg-primary/10 px-2 text-[12px] font-medium text-primary disabled:opacity-40"
      >
        Open
        <ChevronRight className="size-3.5" />
      </button>
    </>
  );
}

function RecordTable({
  rows,
  open,
  empty,
  showWhen = false,
  resetKey = "",
  fill = false,
  onDecide,
  onDecideName,
  onOpenName,
}: {
  rows: RecordRow[];
  open: (id: string) => void;
  empty: string;
  showWhen?: boolean;
  resetKey?: string;
  fill?: boolean;
  onDecide?: (id: string, action: Choice) => void;
  onDecideName?: (name: string, action: "drop" | "allow_direct") => void;
  onOpenName?: (name: string) => void;
}) {
  const [page, setPage] = useState(0);
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>({});
  const groups = useMemo(() => groupRecords(rows), [rows]);
  useEffect(() => {
    setPage(0);
  }, [resetKey]);
  const pages = Math.max(1, Math.ceil(groups.length / pageSize));
  const current = Math.min(page, pages - 1);
  const start = current * pageSize;
  const shown = groups.slice(start, start + pageSize);
  return (
    <div className={fill ? "flex h-full min-h-0 flex-col" : ""}>
      {groups.length === 0 ? (
        <p className="text-sm text-muted-foreground">{empty}</p>
      ) : (
        <div className={fill ? "min-h-0 flex-1 overflow-auto" : "overflow-x-auto"}>
          <table className="w-full min-w-[720px] text-left text-sm">
            <thead className={`text-[11px] tracking-wide text-muted-foreground uppercase ${fill ? "sticky top-0 bg-card" : ""}`}>
              <tr>
                <th className="pb-2 pr-3 font-medium">Name</th>
                <th className="pb-2 pr-3 font-medium">Device</th>
                {showWhen ? <th className="pb-2 pr-3 font-medium">When</th> : null}
                <th className="pb-2 pr-3 font-medium">Open now</th>
                <th className="pb-2 pr-3 font-medium">Collected</th>
                <th className="pb-2 pr-3 font-medium">Suggested</th>
                <th className="pb-2 font-medium"> </th>
              </tr>
            </thead>
            <tbody>
              {shown.map((group) => {
                const alone = group.rows.length === 1 ? group.rows[0] : undefined;
                const expanded = Boolean(openGroups[group.key]);
                return (
                  <Fragment key={group.key}>
                    <tr className="border-t">
                      <td className="py-2.5 pr-3">
                        <span className="block font-medium">{group.name}</span>
                        {alone ? (
                          <span className="block font-mono text-xs text-muted-foreground">{alone.dest}</span>
                        ) : (
                          <button
                            type="button"
                            aria-expanded={expanded}
                            onClick={() => setOpenGroups((current) => ({ ...current, [group.key]: !current[group.key] }))}
                            className="mt-0.5 text-[12px] text-primary hover:underline"
                          >
                            {group.rows.length} addresses
                          </button>
                        )}
                      </td>
                      <td className="py-2.5 pr-3">{group.device}</td>
                      {showWhen ? <td className="py-2.5 pr-3 whitespace-nowrap">{group.at ? hourLabel(group.at) : "—"}</td> : null}
                      <td className="py-2.5 pr-3 tabular-nums">{group.openCount === 0 ? "None" : bytes(group.openBytes)}</td>
                      <td className="py-2.5 pr-3 tabular-nums">{bytes(group.collected)}</td>
                      <td className="py-2.5 pr-3">{group.unanimous ? suggestedLabel(group.category, group.suggestion) : "Mixed"}</td>
                      <td className="py-2.5 text-right whitespace-nowrap">
                        {!group.key.startsWith("one:") ? (
                          <NameActions name={group.name} rows={group.rows} onDecideName={onDecideName} onOpenName={onOpenName} />
                        ) : alone ? (
                          <RecordActions row={alone} open={open} onDecide={onDecide} />
                        ) : null}
                      </td>
                    </tr>
                    {!alone && expanded
                      ? group.rows.map((row) => (
                          <tr key={row.key} className="border-t bg-muted/40">
                            <td className="py-2 pr-3 pl-6">
                              <span className="block font-mono text-xs">{row.dest}</span>
                            </td>
                            <td className="py-2 pr-3 text-[13px]">{row.device}</td>
                            {showWhen ? <td className="py-2 pr-3 whitespace-nowrap text-[13px]">{row.at ? hourLabel(row.at) : "—"}</td> : null}
                            <td className="py-2 pr-3 text-[13px] tabular-nums">{row.openCount === 0 ? "None" : bytes(row.openBytes)}</td>
                            <td className="py-2 pr-3 text-[13px] tabular-nums">{bytes(row.collected)}</td>
                            <td className="py-2 pr-3 text-[13px]">{suggestedLabel(row.category, row.suggestion)}</td>
                            <td className="py-2 text-right whitespace-nowrap">
                              <RecordActions row={row} open={open} onDecide={onDecide} />
                            </td>
                          </tr>
                        ))
                      : null}
                  </Fragment>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      <div className={`flex items-center justify-between gap-3 ${fill ? "shrink-0 border-t bg-card pt-2.5" : "mt-3"}`}>
        <p className="text-[12px] text-muted-foreground">
          {groups.length === 0 ? "0 rows" : `${start + 1}–${Math.min(groups.length, start + shown.length)} of ${groups.length}`}
        </p>
        <div className="flex gap-2">
          <button
            type="button"
            onClick={() => setPage(current - 1)}
            disabled={current === 0}
            className="h-8 rounded-md border px-2.5 text-[12px] font-medium disabled:opacity-40"
          >
            Previous
          </button>
          <button
            type="button"
            onClick={() => setPage(current + 1)}
            disabled={current >= pages - 1}
            className="h-8 rounded-md border px-2.5 text-[12px] font-medium disabled:opacity-40"
          >
            Next
          </button>
        </div>
      </div>
    </div>
  );
}

function localInputToUTC(value: string) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toISOString();
}

function RecordTabs({
  cards,
  live,
  open,
  deviceId = "",
  historyCards,
  fill = false,
  onDecide,
  onDecideName,
  onOpenName,
  filterQuery = "",
  filterKind = "all",
}: {
  cards: Card[];
  live: LiveConn[];
  open: (id: string) => void;
  deviceId?: string;
  historyCards?: Card[];
  fill?: boolean;
  onDecide?: (id: string, action: Choice) => void;
  onDecideName?: (name: string, action: "drop" | "allow_direct") => void;
  onOpenName?: (name: string) => void;
  filterQuery?: string;
  filterKind?: string;
}) {
  const [tab, setTab] = useState<"live" | "history">("live");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const inverted = Boolean(from && to && from > to);
  const query = useMemo(() => {
    if (tab !== "history" || inverted) return "";
    const params = new URLSearchParams({ names: "1" });
    if (deviceId) params.set("device", deviceId);
    const start = localInputToUTC(from);
    const end = localInputToUTC(to);
    if (start) params.set("from", start);
    if (end) params.set("to", end);
    return params.toString();
  }, [tab, inverted, deviceId, from, to]);
  const samples = useHistory(query);
  const liveRows = liveRecords(cards, live);
  const historyRows = useMemo(() => {
    const byID = new Map(samples.map((sample) => [sample.patternId, sample]));
    const base = historyCards ?? cards.filter((card) => openOf(live, card.destination, card.addr).count === 0);
    const timed = Boolean(from || to);
    return base
      .filter((card) => !timed || byID.has(card.id))
      .map((card) => {
        const sample = byID.get(card.id);
        const openNow = openOf(live, card.destination, card.addr);
        return {
          key: card.id,
          name: shortName(card),
          dest: leavingTo(card),
          device: card.deviceName,
          addr: card.addr ?? "",
          at: sample?.at,
          openBytes: openNow.bytes,
          openCount: openNow.count,
          collected: timed && sample ? sample.leftBytes : card.metrics.beforeBytes,
          suggestion: card.suggestion,
          category: card.category,
          action: card.action,
          status: card.status,
          selfSigned: Boolean(card.selfSigned),
          reason: card.llmReason ?? "",
          cardId: card.id,
        };
      })
      .sort((a, b) => (b.at ?? "").localeCompare(a.at ?? "") || b.collected - a.collected || a.name.localeCompare(b.name));
  }, [samples, historyCards, cards, live, from, to]);
  const visibleLive = useMemo(() => liveRows.filter((row) => rowMatches(row, filterQuery, filterKind)), [liveRows, filterQuery, filterKind]);
  const visibleHistory = useMemo(() => historyRows.filter((row) => rowMatches(row, filterQuery, filterKind)), [historyRows, filterQuery, filterKind]);
  const filtering = filterQuery.trim() !== "" || filterKind !== "all";
  const tabClass = (selected: boolean) =>
    `border-b-2 px-1 pb-2 text-[13px] font-medium ${selected ? "border-primary text-foreground" : "border-transparent text-muted-foreground"}`;
  return (
    <section className={`rounded-xl border bg-card shadow-[0_1px_2px_rgba(16,24,40,0.04)] ${fill ? "flex h-full min-h-0 flex-col" : ""}`}>
      <header className="flex shrink-0 flex-wrap items-end justify-between gap-3 border-b px-4 pt-3">
        <div role="tablist" aria-label="Records" className="flex gap-4">
          <button type="button" role="tab" aria-selected={tab === "live"} className={tabClass(tab === "live")} onClick={() => setTab("live")}>
            Live
          </button>
          <button type="button" role="tab" aria-selected={tab === "history"} className={tabClass(tab === "history")} onClick={() => setTab("history")}>
            History
          </button>
        </div>
        {tab === "history" ? (
          <div className="mb-2 flex flex-wrap items-center gap-2">
            <label className="flex items-center gap-2 text-[12px] text-muted-foreground">
              From
              <input
                type="datetime-local"
                aria-label="From"
                value={from}
                onChange={(event) => setFrom(event.target.value)}
                className="h-8 rounded-md border bg-background px-2 text-[12px] text-foreground"
              />
            </label>
            <label className="flex items-center gap-2 text-[12px] text-muted-foreground">
              To
              <input
                type="datetime-local"
                aria-label="To"
                value={to}
                onChange={(event) => setTo(event.target.value)}
                className="h-8 rounded-md border bg-background px-2 text-[12px] text-foreground"
              />
            </label>
          </div>
        ) : null}
      </header>
      <div className={fill ? "flex min-h-0 flex-1 flex-col p-3" : "p-4"} role="tabpanel">
        {tab === "history" && inverted ? (
          <p className="text-sm text-muted-foreground">The start is after the end.</p>
        ) : (
          <RecordTable
            rows={tab === "live" ? visibleLive : visibleHistory}
            open={open}
            showWhen={tab === "history"}
            resetKey={`${tab}|${from}|${to}|${filterQuery}|${filterKind}`}
            empty={filtering ? "Nothing matches this search." : tab === "live" ? "No open connection." : "Nothing collected in this time."}
            fill={fill}
            onDecide={onDecide}
            onDecideName={onDecideName}
            onOpenName={onOpenName}
          />
        )}
      </div>
    </section>
  );
}

function RecordsPage({
  cards,
  live,
  open,
  onDecide,
  onDecideName,
  onOpenName,
}: {
  cards: Card[];
  live: LiveConn[];
  open: (id: string) => void;
  onDecide: (id: string, action: Choice) => void;
  onDecideName: (name: string, action: "drop" | "allow_direct") => void;
  onOpenName: (name: string) => void;
}) {
  const [query, setQuery] = useState("");
  const [kind, setKind] = useState("all");
  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <div className="flex shrink-0 flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[22px] font-semibold tracking-tight">Records</h1>
          <p className="mt-1 max-w-2xl text-[13px] leading-5 text-muted-foreground">
            Search by IP, name, or drop kind. Addresses that share a name are one row.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <input
            type="search"
            aria-label="Search by IP or name"
            placeholder="IP or name"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            className="h-9 w-56 rounded-md border bg-background px-3 text-sm"
          />
          <label className="flex items-center gap-2 text-[13px] text-muted-foreground">
            Drop kind
            <select
              aria-label="Drop kind"
              value={kind}
              onChange={(event) => setKind(event.target.value)}
              className="h-9 rounded-md border bg-background px-2 text-sm text-foreground"
            >
              <option value="all">All</option>
              <option value="drop">Drop</option>
              <option value="allow">Allow</option>
              <option value="notyet">Not yet</option>
              <option value="self-signed">Self-signed</option>
              <option value="none">No certificate</option>
            </select>
          </label>
        </div>
      </div>
      <div className="min-h-0 flex-1">
        <RecordTabs fill cards={cards} live={live} open={open} onDecide={onDecide} onDecideName={onDecideName} onOpenName={onOpenName} filterQuery={query} filterKind={kind} />
      </div>
    </div>
  );
}

function BackLink({ onClick, children }: { onClick: () => void; children: ReactNode }) {
  return (
    <button type="button" onClick={onClick} className="inline-flex items-center gap-1 text-[13px] font-medium text-primary hover:underline">
      <ChevronLeft className="size-3.5" />
      {children}
    </button>
  );
}

function DeviceView({
  device,
  cards,
  live,
  via,
  open,
  onDecide,
  onDecideName,
  onOpenName,
  onBack,
  onVia,
  onSteer,
}: {
  device: Device;
  cards: Card[];
  live: LiveConn[];
  via?: ViaPath;
  open: (id: string) => void;
  onDecide: (id: string, action: Choice) => void;
  onDecideName: (name: string, action: "drop" | "allow_direct") => void;
  onOpenName: (name: string) => void;
  onBack: () => void;
  onVia: (id: string, on: boolean) => void;
  onSteer: (on: boolean) => Promise<void>;
}) {
  const samples = useHistory(`device=${encodeURIComponent(device.id)}`);
  const [steerBusy, setSteerBusy] = useState(false);
  const now = deviceOpen(live, device.addr);
  const left = cards.reduce((sum, card) => sum + card.metrics.beforeBytes, 0);
  const kept = cards.reduce((sum, card) => sum + card.metrics.afterBytes, 0);
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <div className="flex shrink-0 items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-baseline gap-3">
            <BackLink onClick={onBack}>All devices</BackLink>
            <h1 className="truncate text-lg font-semibold tracking-tight">{device.name}</h1>
          </div>
          <p className="mt-0.5 truncate text-[12px] text-muted-foreground">{deviceIdentity(device)}</p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {via?.steer ? (
            <button
              type="button"
              disabled={steerBusy}
              onClick={() => {
                setSteerBusy(true);
                onSteer(false).finally(() => setSteerBusy(false));
              }}
              className="h-8 rounded-md bg-primary px-2.5 text-[12px] font-medium text-primary-foreground disabled:opacity-60"
            >
              {steerBusy ? "Waiting for permission…" : "Return to the router"}
            </button>
          ) : null}
          <button
            type="button"
            title={devicePath(device, via)}
            onClick={() => onVia(device.id, !device.viaAno)}
            className={
              device.viaAno
                ? "h-8 rounded-md border px-2.5 text-[12px] font-medium text-muted-foreground"
                : "h-8 rounded-md bg-primary px-2.5 text-[12px] font-medium text-primary-foreground"
            }
          >
            {device.viaAno ? "Remove" : "Add to Ano"}
          </button>
        </div>
      </div>
      <div className="grid shrink-0 grid-cols-2 gap-3 lg:grid-cols-4">
        <Stat label="Open now" value={now.names.length === 0 ? "None" : bytes(now.bytes)} note={now.names.length === 0 ? "No open connection" : `${now.names.length} open`} tone="#1F6FEB" />
        <Stat label="Collected" value={bytes(left)} note="Recorded so far" tone="#FF9830" />
        <Stat label="Bytes kept" value={bytes(kept)} note="Refused after a drop" tone="#73BF69" />
        <Stat label="Names" value={String(cards.length)} note="Open a row for its chart" />
      </div>
      <div className="grid min-h-0 flex-1 basis-0 gap-3 xl:grid-cols-5">
        <div className="min-h-0 xl:col-span-3">
          <HistoryPanel fit samples={samples} hint="Last 24 hours" />
        </div>
        <div className="min-h-0 overflow-hidden xl:col-span-2">
          <Panel title="Collected bytes that left" hint="Largest names" className="flex h-full min-h-0 flex-col" bodyClassName="relative min-h-0 flex-1 p-3">
            <div className="absolute inset-3">
              <SharePie slices={shareSlices(cards)} className="h-full" legend="side" />
            </div>
          </Panel>
        </div>
      </div>
      <div className="min-h-0 flex-1 basis-0">
        <RecordTabs fill cards={cards} live={live} open={open} deviceId={device.id} onDecide={onDecide} onDecideName={onDecideName} onOpenName={onOpenName} />
      </div>
    </div>
  );
}

type Choice = "drop" | "allow_direct" | "allow_envoy" | "ignore";

function enforcedChoice(card: Card): Choice | "pending" {
  if (card.status === "rejected") return "ignore";
  if (card.status === "approved" && (card.action === "drop" || card.action === "allow_direct" || card.action === "allow_envoy")) {
    return card.action;
  }
  return "pending";
}

function enforcedLabel(choice: Choice | "pending") {
  if (choice === "drop") return "Dropped";
  if (choice === "allow_direct") return "Going direct";
  if (choice === "allow_envoy") return "Through Envoy";
  if (choice === "ignore") return "Set aside";
  return "Not enforced";
}

function ChoicePanel({ card, onDecide }: { card: Card; onDecide: (id: string, action: Choice) => void }) {
  const current = enforcedChoice(card);
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <ChoiceButton label="Drop" meaning="Drop it. This name never reaches the router." active={current === "drop"} tone="drop" onClick={() => onDecide(card.id, "drop")} />
      <ChoiceButton label="Allow" meaning="Let it go direct. It skips Ano and Envoy." active={current === "allow_direct"} tone="allow" onClick={() => onDecide(card.id, "allow_direct")} />
      <span className="px-1.5 text-[11px] text-muted-foreground">
        Now <span className="font-medium text-foreground">{enforcedLabel(current)}</span>
      </span>
      <ChoiceButton label="Answer here" meaning="Via Envoy. This machine answers. It does not call the vendor." active={current === "allow_envoy"} tone="envoy" onClick={() => onDecide(card.id, "allow_envoy")} />
      <ChoiceButton label="Set aside" meaning="Not a leak. Nothing is enforced." active={current === "ignore"} tone="ignore" onClick={() => onDecide(card.id, "ignore")} />
    </div>
  );
}

function ChoiceButton({
  label,
  meaning,
  active,
  tone,
  onClick,
}: {
  label: string;
  meaning: string;
  active: boolean;
  tone: "drop" | "allow" | "envoy" | "ignore";
  onClick: () => void;
}) {
  const on = {
    drop: "border-[#F2495C] bg-[#F2495C] text-white",
    allow: "border-[#73BF69] bg-[#73BF69] text-[#10210f]",
    envoy: "border-[#5794F2] bg-[#5794F2] text-white",
    ignore: "border-foreground/20 bg-muted text-foreground",
  }[tone];
  const off = "border bg-card text-foreground/80 hover:bg-muted hover:text-foreground";
  return (
    <button
      type="button"
      aria-pressed={active}
      title={meaning}
      onClick={onClick}
      className={`h-7 rounded-md border px-2 text-[12px] leading-none ${active ? on : off}`}
    >
      {label}
      <span className="sr-only">. {meaning}{active ? ". In force" : ""}</span>
    </button>
  );
}

function plainSentence(sentence: string) {
  let cut = sentence.length;
  for (const marker of [" Qwen:", " AI Inference:"]) {
    const at = sentence.indexOf(marker);
    if (at >= 0 && at < cut) cut = at;
  }
  return sentence.slice(0, cut).trim().replace(/\s*Suggested action:.*$/, "").trim();
}

function shownOutcome(outcome: string) {
  if (outcome === "qwen") return "AI Inference";
  return outcome.replaceAll("_", " ");
}

function shownName(name: string) {
  return name.replaceAll("hBar", "Ano");
}

type QwenReadout = {
  inference: string;
  cadence: string;
  sendTime: string;
  similar: string;
  protocol: string;
};

function tidyFact(value: string) {
  return value.replace(/^[\s.]+/, "").replace(/[\s.]+$/, "");
}

function readQwen(reason: string): QwenReadout {
  const markers: { key: keyof Omit<QwenReadout, "inference">; label: string }[] = [
    { key: "cadence", label: "Cadence:" },
    { key: "sendTime", label: "Send time:" },
    { key: "similar", label: "Similar traffic:" },
    { key: "protocol", label: "Protocol:" },
  ];
  const found = markers
    .map((marker) => ({ ...marker, at: reason.indexOf(marker.label) }))
    .filter((marker) => marker.at >= 0)
    .sort((a, b) => a.at - b.at);
  const inferenceEnd = found.length > 0 ? found[0].at : reason.length;
  const readout: QwenReadout = {
    inference: tidyFact(reason.slice(0, inferenceEnd)),
    cadence: "",
    sendTime: "",
    similar: "",
    protocol: "",
  };
  for (let i = 0; i < found.length; i++) {
    const start = found[i].at + found[i].label.length;
    const end = i + 1 < found.length ? found[i + 1].at : reason.length;
    readout[found[i].key] = tidyFact(reason.slice(start, end));
  }
  return readout;
}

function QwenRead({ reason, protocol }: { reason: string; protocol?: string }) {
  const parts = readQwen(reason);
  const facts = [
    { label: "How often", value: parts.cadence },
    { label: "When it sends", value: parts.sendTime },
    { label: "Similar on this device", value: parts.similar },
    { label: "Protocol", value: parts.protocol || protocol || "" },
  ].filter((fact) => fact.value);
  return (
    <section className="max-w-3xl overflow-hidden rounded-xl border bg-card">
      <div className="px-4 py-3">
        <p className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">AI Inference, past 24 hours</p>
        {parts.inference ? <p className="mt-1 text-sm leading-6">{parts.inference}</p> : null}
      </div>
      {facts.length > 0 ? (
        <dl className="grid sm:grid-cols-2">
          {facts.map((fact) => (
            <div key={fact.label} className="border-t px-4 py-3 sm:odd:border-r">
              <dt className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">{fact.label}</dt>
              <dd className="mt-1 text-sm leading-6">{fact.value}</dd>
            </div>
          ))}
        </dl>
      ) : null}
    </section>
  );
}

function NameView({
  name,
  cards,
  live,
  open,
  onDecideName,
  onBack,
}: {
  name: string;
  cards: Card[];
  live: LiveConn[];
  open: (id: string) => void;
  onDecideName: (name: string, action: "drop" | "allow_direct") => void;
  onBack: () => void;
}) {
  const collected = cards.reduce((sum, card) => sum + card.metrics.beforeBytes, 0);
  const openNow = cards.reduce((sum, card) => sum + openOf(live, card.destination, card.addr).bytes, 0);
  const openCount = cards.reduce((sum, card) => sum + openOf(live, card.destination, card.addr).count, 0);
  const dropped = cards.length > 0 && cards.every((card) => card.status === "approved" && card.action === "drop");
  const allowed = cards.length > 0 && cards.every((card) => card.status === "approved" && card.action === "allow_direct");
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <BackLink onClick={onBack}>Records</BackLink>
          <h1 className="mt-2 text-xl font-semibold tracking-tight">{name}</h1>
          <p className="mt-1 max-w-2xl text-[13px] leading-5 text-muted-foreground">
            {cards.length} {cards.length === 1 ? "address" : "addresses"}. Drop and Allow apply to every address with this name, including addresses that show up later.
          </p>
        </div>
        <div className="flex gap-2">
          <button
            type="button"
            aria-pressed={dropped}
            onClick={() => onDecideName(name, "drop")}
            className={`h-8 rounded-md border px-3 text-[13px] ${dropped ? "border-[#F2495C] bg-[#F2495C] text-white" : "bg-card"}`}
          >
            Drop
          </button>
          <button
            type="button"
            aria-pressed={allowed}
            onClick={() => onDecideName(name, "allow_direct")}
            className={`h-8 rounded-md border px-3 text-[13px] ${allowed ? "border-[#73BF69] bg-[#73BF69] text-[#10210f]" : "bg-card"}`}
          >
            Allow
          </button>
        </div>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <Stat label="Open now" value={openCount === 0 ? "None" : bytes(openNow)} note={openCount === 0 ? "No open connection" : `${openCount} open`} tone="#1F6FEB" />
        <Stat label="Collected" value={bytes(collected)} note="Every address with this name" tone="#FF9830" />
      </div>
      <section className="overflow-hidden rounded-xl border bg-card">
        <table className="w-full text-left text-sm">
          <thead className="text-[11px] tracking-wide text-muted-foreground uppercase">
            <tr>
              <th className="px-4 py-2 font-medium">Address</th>
              <th className="px-4 py-2 font-medium">Device</th>
              <th className="px-4 py-2 font-medium">Collected</th>
              <th className="px-4 py-2 font-medium"> </th>
            </tr>
          </thead>
          <tbody>
            {cards.map((card) => (
              <tr key={card.id} className="border-t">
                <td className="px-4 py-2 font-mono text-xs">{leavingTo(card)}</td>
                <td className="px-4 py-2">{card.deviceName}</td>
                <td className="px-4 py-2 tabular-nums">{bytes(card.metrics.beforeBytes)}</td>
                <td className="px-4 py-2 text-right">
                  <button
                    type="button"
                    onClick={() => open(card.id)}
                    className="inline-flex h-7 items-center gap-0.5 rounded-md border border-primary/30 bg-primary/10 px-2 text-[12px] font-medium text-primary"
                  >
                    Open
                    <ChevronRight className="size-3.5" />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </div>
  );
}

function PatternView({
  card,
  live,
  onDecide,
  onDevices,
  onDevice,
}: {
  card: Card;
  live: LiveConn[];
  onDecide: (id: string, action: Choice) => void;
  onDevices: () => void;
  onDevice: () => void;
}) {
  const samples = useHistory(`pattern=${encodeURIComponent(card.id)}`);
  const rows = historyRows(samples);
  const now = openOf(live, card.destination, card.addr);
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <p className="flex flex-wrap items-center gap-1 text-[13px] text-muted-foreground">
            <button type="button" onClick={onDevices} className="font-medium text-primary hover:underline">
              Devices
            </button>
            <ChevronRight className="size-3.5" />
            <button type="button" onClick={onDevice} className="font-medium text-primary hover:underline">
              {card.deviceName}
            </button>
            <ChevronRight className="size-3.5" />
            <span>{shortName(card)}</span>
          </p>
          <h1 className="mt-2 text-xl font-semibold tracking-tight">{shortName(card)}</h1>
          <p className="mt-1 font-mono text-xs text-muted-foreground">
            {leavingTo(card)}
            {card.protocol ? ` · ${card.protocol}` : ""}
          </p>
        </div>
      </div>
      {card.category === "stream" ? (
        <p className="max-w-3xl text-sm leading-6 text-muted-foreground">{card.evidence} Streaming stays direct.</p>
      ) : (
        <ChoicePanel card={card} onDecide={onDecide} />
      )}
      <p className="max-w-3xl text-base leading-7">{plainSentence(card.sentence)}</p>
      {card.llmReason ? <QwenRead reason={card.llmReason} protocol={card.protocol} /> : null}
      <p className="max-w-3xl text-sm leading-6 text-muted-foreground">{card.consequence}</p>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Stat label="Open now" value={now.count === 0 ? "None" : bytes(now.bytes)} note={now.count === 0 ? "No open connection" : `${now.count} open connection${now.count === 1 ? "" : "s"}`} />
        <Stat label="Collected" value={bytes(card.metrics.beforeBytes)} note="Everything recorded for this name" />
        <Stat label="Lookups collected" value={String(card.metrics.beforeLookups)} note={`About every ${card.metrics.everySeconds}s before the choice`} />
        <Stat label="Bytes kept" value={bytes(card.metrics.afterBytes)} note={card.metrics.dropped ? "Kept on this machine" : "Nothing kept yet"} />
      </div>
      <div className="grid gap-4 xl:grid-cols-2">
        <HistoryPanel samples={samples} hint="Each hour this name went out. Use the hours to choose drop or allow." />
        <Panel title="Sends over time" hint="How often this name went out, and how often a drop refused it.">
          <BytesLine
            rows={rows}
            lines={[
              { key: "lookups", name: "Sends that left", color: "#5794F2" },
              { key: "refused", name: "Sends kept", color: "#F2495C" },
            ]}
            unit="count"
          />
        </Panel>
      </div>
      <div className="grid gap-4 xl:grid-cols-2">
        <Panel title="This name, before and after">
          <SharePie
            slices={[
              { name: "Bytes left", value: card.metrics.beforeBytes },
              { name: "Bytes kept", value: card.metrics.afterBytes },
            ]}
          />
        </Panel>
        <Panel title="Why this is on the list">
          <p className="text-sm leading-6">{card.evidence}</p>
          <p className="mt-3 text-sm leading-6">Suggested action: {suggestedLabel(card.category, card.suggestion)}</p>
          {card.category !== "stream" && card.suggestion !== "drop" && card.suggestion !== "allow_direct" && card.suggestion !== "allow" ? (
            <p className="mt-1 text-[12px] leading-5 text-muted-foreground">AI Inference fills this after it reads the name. Drop or Allow is your choice, and it overrides the suggestion.</p>
          ) : null}
          <p className="mt-3 text-sm">
            Status: {card.metrics.dropped ? "Dropped" : card.status === "approved" ? `Allowed (${card.action})` : "Waiting for you"}
          </p>
        </Panel>
      </div>
    </div>
  );
}

function Decisions({ decisions }: { decisions: Decision[] }) {
  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Decisions</h1>
        <p className="mt-1 text-sm text-muted-foreground">Collected choices. This log is not the open connections.</p>
      </div>
      <Panel title="Log">
        <DecisionTable decisions={decisions} />
      </Panel>
    </div>
  );
}

function DecisionList({ decisions }: { decisions: Decision[] }) {
  if (decisions.length === 0) {
    return <p className="text-sm text-muted-foreground">No decisions yet. Open a name and choose drop or allow.</p>;
  }
  return (
    <ul>
      {decisions.map((item) => (
        <li key={`${item.at}-${item.destination}`} className="border-b py-2.5 last:border-b-0">
          <div className="flex items-baseline justify-between gap-3">
            <p className="text-sm font-medium">{names[item.destination] ?? item.destination}</p>
            <p className="shrink-0 text-[11px] text-muted-foreground">{when(item.at)}</p>
          </div>
          <p className="mt-0.5 text-[12px] capitalize text-muted-foreground">
            {shownName(item.deviceName)} · {shownOutcome(item.outcome)}
          </p>
        </li>
      ))}
    </ul>
  );
}

function DecisionTable({ decisions }: { decisions: Decision[] }) {
  if (decisions.length === 0) {
    return <p className="text-sm text-muted-foreground">No decisions yet. Open a name and choose drop or allow.</p>;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[720px] text-left text-sm">
        <thead className="text-xs tracking-wide text-muted-foreground uppercase">
          <tr>
            <th className="py-2 pr-3 font-medium">When</th>
            <th className="py-2 pr-3 font-medium">Device</th>
            <th className="py-2 pr-3 font-medium">Name</th>
            <th className="py-2 pr-3 font-medium">Outcome</th>
            <th className="py-2 font-medium">What changed</th>
          </tr>
        </thead>
        <tbody>
          {decisions.map((item) => (
            <tr key={`${item.at}-${item.destination}`} className="border-t">
              <td className="py-3 pr-3 whitespace-nowrap">{when(item.at)}</td>
              <td className="py-3 pr-3">{shownName(item.deviceName)}</td>
              <td className="py-3 pr-3">{names[item.destination] ?? item.destination}</td>
              <td className="py-3 pr-3 capitalize">{shownOutcome(item.outcome)}</td>
              <td className="py-3 text-muted-foreground">{item.detail.replaceAll("Qwen", "AI Inference").replaceAll("hBar", "Ano")}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function EnvoyView({
  envoy,
  cards,
  live,
  host,
  setHost,
  probe,
  onProbe,
  open,
  onDecide,
  onDecideName,
  onOpenName,
}: {
  envoy: Envoy;
  cards: Card[];
  live: LiveConn[];
  host: string;
  setHost: (value: string) => void;
  probe: string;
  onProbe: (event: FormEvent) => void;
  open: (id: string) => void;
  onDecide: (id: string, action: Choice) => void;
  onDecideName: (name: string, action: "drop" | "allow_direct") => void;
  onOpenName: (name: string) => void;
}) {
  const active = cards.filter((card) => card.status === "approved");
  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Envoy</h1>
        <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">
          Open now is a connection Envoy is carrying. Approved names are collected rules.
        </p>
      </div>
      <div className="grid gap-3 sm:grid-cols-3">
        <Stat label="Process" value={envoy.running ? "Running" : "Down"} note={envoy.message || "No message"} />
        <Stat label="Listen" value={visibleListen(envoy.listen)} note="Household traffic aimed at Ano" />
        <Stat label="Open now" value={String(live.length)} note={live.length === 0 ? "No open connection" : `${bytes(live.reduce((sum, conn) => sum + conn.bytes, 0))} on open connections`} />
      </div>
      <Panel title="Try a name">
        <form onSubmit={onProbe} className="flex flex-col gap-3 sm:flex-row">
          <Input value={host} onChange={(event) => setHost(event.target.value)} aria-label="Hostname to check" className="sm:max-w-md" />
          <Button type="submit">Check through Envoy</Button>
        </form>
        {probe ? <p className="mt-3 text-sm leading-6">{probe}</p> : null}
      </Panel>
      <RecordTabs cards={cards} live={live} open={open} historyCards={active} onDecide={onDecide} onDecideName={onDecideName} onOpenName={onOpenName} />
    </div>
  );
}
