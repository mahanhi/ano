"use client";

import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";
import { ArrowRightLeft, ChevronLeft, ChevronRight, Cpu, House, LayoutDashboard, ListChecks, Radio, Route, Router } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { BytesLine, Panel, SharePie } from "@/components/charts";
import { RouteMap } from "@/components/route-map";
import { ProxyMap } from "@/components/proxy-map";
import { SystemDesign } from "@/components/system-design";
import { AdminPage } from "@/components/admin";

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
  destination: string;
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
  observations: number;
  metrics: Metrics;
};

type Device = { id: string; name: string; room: string; kind: string; viaAno?: boolean };
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
export type AgentStatus = {
  name: string;
  mode: "passive" | "active";
  collecting: boolean;
  reviewed: number;
  detail: string;
  model: { kind: string; name: string; url: string; ready: boolean; detail: string };
};
export type DashboardState = {
  settings: { speakerOnly: boolean; mode?: "passive" | "active" };
  cards: Card[];
  devices: Device[];
  decisions: Decision[];
  series: Sample[];
  envoy: Envoy;
  agent?: AgentStatus;
  via?: ViaPath;
};

type View =
  | { kind: "overview" }
  | { kind: "route" }
  | { kind: "system" }
  | { kind: "devices" }
  | { kind: "admin" }
  | { kind: "device"; id: string }
  | { kind: "pattern"; id: string }
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

function bytes(n: number) {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)} MB`;
  if (n >= 1000) return `${(n / 1000).toFixed(1)} KB`;
  return `${n} B`;
}

const pacific = { timeZone: "America/Los_Angeles" } as const;

function clock(at: string) {
  return new Date(at).toLocaleTimeString("en-US", { ...pacific, hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function when(at: string) {
  return new Date(at).toLocaleString("en-US", { ...pacific, month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function sumLine(samples: Sample[]) {
  const rows = new Map<string, { time: string; left: number; kept: number; lookups: number; refused: number }>();
  for (const sample of samples) {
    const row = rows.get(sample.at) ?? { time: clock(sample.at), left: 0, kept: 0, lookups: 0, refused: 0 };
    row.left += sample.leftBytes;
    row.kept += sample.keptBytes;
    row.lookups += sample.lookups;
    row.refused += sample.refused;
    rows.set(sample.at, row);
  }
  return [...rows.values()];
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
    setState({ ...body, series: body.series ?? [] });
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

  const visible = useMemo(() => {
    if (!state) return [];
    return state.settings.speakerOnly ? state.cards.filter((card) => card.kind === "speaker") : state.cards;
  }, [state]);

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

  async function setSpeakerOnly(speakerOnly: boolean) {
    await fetch("/api/settings", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ speakerOnly }),
    });
    setView({ kind: "overview" });
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

  const selected = view.kind === "pattern" ? visible.find((card) => card.id === view.id) : undefined;
  const device = view.kind === "device" ? state.devices.find((item) => item.id === view.id) : undefined;

  return (
    <div className="flex h-dvh justify-center bg-[oklch(0.93_0.012_260)] p-3 text-foreground md:p-5">
      <div className="flex min-h-0 w-full max-w-[1440px] overflow-hidden rounded-2xl border bg-background shadow-[0_16px_48px_rgba(16,24,40,0.1)]">
      <aside className="flex w-16 shrink-0 flex-col border-r bg-sidebar md:w-56">
        <div className="flex items-center gap-2.5 px-3 py-5 md:px-4">
          <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-primary text-[13px] font-semibold text-primary-foreground">A</span>
          <div className="hidden min-w-0 md:block">
            <p className="text-sm font-semibold tracking-tight">Ano</p>
            <p className="text-[11px] text-muted-foreground">Home traffic</p>
          </div>
        </div>
        <nav className="flex-1 space-y-0.5 overflow-y-auto px-2 pb-3 md:px-3">
          <NavButton icon={LayoutDashboard} active={view.kind === "overview"} onClick={() => setView({ kind: "overview" })}>
            Overview
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

      <main className="min-h-0 min-w-0 flex-1 overflow-y-auto px-5 py-5 md:px-8 md:py-6">
          {error ? <p className="mb-4 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm">{error}</p> : null}
          {view.kind === "overview" ? (
            <Overview
              cards={visible}
              series={state.series}
              decisions={state.decisions}
              envoy={state.envoy}
              speakerOnly={state.settings.speakerOnly}
              open={(id) => setView({ kind: "pattern", id })}
              showRoute={() => setView({ kind: "route" })}
            />
          ) : null}
          {view.kind === "route" ? (
            <RoutePage cards={visible} speakerOnly={state.settings.speakerOnly} open={(id) => setView({ kind: "pattern", id })} />
          ) : null}
          {view.kind === "system" ? <SystemDesign agent={state.agent} /> : null}
          {view.kind === "admin" ? <AdminPage open={(id) => setView({ kind: "device", id })} /> : null}
          {view.kind === "devices" ? (
            <DevicesPage devices={state.devices} cards={state.cards} speakerOnly={state.settings.speakerOnly} open={(id) => setView({ kind: "device", id })} />
          ) : null}
          {view.kind === "device" && device ? (
            <DeviceView
              device={device}
              cards={state.cards.filter((card) => card.deviceId === device.id)}
              series={state.series}
              via={state.via}
              open={(id) => setView({ kind: "pattern", id })}
              onBack={() => setView({ kind: "devices" })}
              onDecide={decide}
              onVia={setVia}
              onSteer={setSteer}
            />
          ) : null}
          {view.kind === "pattern" && selected ? (
            <PatternView
              card={selected}
              series={state.series.filter((sample) => sample.patternId === selected.id)}
              onDecide={decide}
              onDevices={() => setView({ kind: "devices" })}
              onDevice={() => setView({ kind: "device", id: selected.deviceId })}
            />
          ) : null}
          {view.kind === "decisions" ? <Decisions decisions={state.decisions} /> : null}
          {view.kind === "proxy" ? <ProxyMap /> : null}
          {view.kind === "envoy" ? (
            <EnvoyView envoy={state.envoy} cards={visible} host={host} setHost={setHost} probe={probe} onProbe={runProbe} />
          ) : null}
        </main>
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
  series,
  decisions,
  envoy,
  speakerOnly,
  open,
  showRoute,
}: {
  cards: Card[];
  series: Sample[];
  decisions: Decision[];
  envoy: Envoy;
  speakerOnly: boolean;
  open: (id: string) => void;
  showRoute: () => void;
}) {
  const ids = new Set(cards.map((card) => card.id));
  const samples = series.filter((sample) => ids.has(sample.patternId));
  const rows = sumLine(samples);
  const left = cards.reduce((sum, card) => sum + card.metrics.beforeBytes, 0);
  const kept = cards.reduce((sum, card) => sum + card.metrics.afterBytes, 0);
  const refused = cards.reduce((sum, card) => sum + card.metrics.afterLookups, 0);
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[22px] font-semibold tracking-tight">Overview</h1>
          <p className="mt-1 max-w-2xl text-[13px] leading-5 text-muted-foreground">
            Passive mode collects every device, including the protocol each send uses. AI Inference chooses drop or allow per device from that device's past 24 hours. Active mode is what enforces that choice.
          </p>
        </div>
        <span className="rounded-full border bg-card px-2.5 py-1 text-[12px] text-muted-foreground">
          {envoy.running ? "Live" : "Envoy down"}
        </span>
      </div>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Stat label="Bytes left" value={bytes(left)} note="Recorded before a drop, or still leaving" tone="#FF9830" />
        <Stat label="Bytes kept" value={bytes(kept)} note="Refused at Ano after a drop" tone="#73BF69" />
        <Stat label="Lookups refused" value={String(refused)} note="Speaker checks that did not go out" tone="#F2495C" />
        <Stat label="Envoy" value={envoy.running ? "Running" : "Down"} note={envoy.running ? `${envoy.routes} routes on ${envoy.listen}` : envoy.message} tone="#5794F2" />
      </div>
      <Panel title="Bytes over time" hint="Left the house vs kept here">
        <BytesLine
          rows={rows}
          lines={[
            { key: "left", name: "Bytes left", color: "#FF9830" },
            { key: "kept", name: "Bytes kept", color: "#73BF69" },
          ]}
        />
      </Panel>
      <div className="grid gap-4 xl:grid-cols-2">
        <Panel title="Where the bytes went" hint="Share of bytes that left, by name">
          <SharePie slices={cards.map((card) => ({ name: shortName(card), value: card.metrics.beforeBytes }))} />
        </Panel>
        <Panel title="What Ano kept" hint="Share of refused bytes, by name">
          <SharePie slices={cards.map((card) => ({ name: shortName(card), value: card.metrics.afterBytes }))} />
        </Panel>
      </div>
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.4fr)_minmax(280px,0.8fr)]">
        <Panel title="Names on the speaker" hint="Open one for its own charts">
          <div className="overflow-x-auto">
            <table className="w-full min-w-[520px] text-left text-sm">
              <thead className="text-[11px] tracking-wide text-muted-foreground uppercase">
                <tr>
                  <th className="pb-2 pr-3 font-medium">Name</th>
                  <th className="pb-2 pr-3 font-medium">Device</th>
                  <th className="pb-2 pr-3 font-medium">Left</th>
                  <th className="pb-2 pr-3 font-medium">Kept</th>
                  <th className="pb-2 font-medium">Now</th>
                </tr>
              </thead>
              <tbody>
                {cards.map((card) => (
                  <tr key={card.id} className="border-t">
                    <td className="py-3 pr-3">
                      <button type="button" className="inline-flex items-center gap-1 text-left font-medium text-primary hover:underline" onClick={() => open(card.id)}>
                        {shortName(card)}
                        <ChevronRight className="size-3.5" />
                        <span className="sr-only">Open this name</span>
                      </button>
                      <p className="text-xs text-muted-foreground">{card.destination}</p>
                    </td>
                    <td className="py-3 pr-3">{card.deviceName}</td>
                    <td className="py-3 pr-3 tabular-nums">{bytes(card.metrics.beforeBytes)}</td>
                    <td className="py-3 pr-3 tabular-nums">{bytes(card.metrics.afterBytes)}</td>
                    <td className="py-3">
                      <StatusPill card={card} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Panel>
        <Panel title="Latest decisions">
          <DecisionList decisions={decisions.slice(0, 6)} />
        </Panel>
      </div>
      <Panel title="Path out of the house" hint="Speaker, Wi-Fi, Ano, router, modem">
        <RouteMap cards={cards} speakerOnly={speakerOnly} onOpen={open} compact />
        <button type="button" onClick={showRoute} className="mt-3 text-[13px] text-primary hover:underline">
          Open the full route
        </button>
      </Panel>
    </div>
  );
}

function RoutePage({ cards, speakerOnly, open }: { cards: Card[]; speakerOnly: boolean; open: (id: string) => void }) {
  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Route</h1>
        <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">
          The kitchen speaker reaches the outside world only by crossing Wi-Fi, then Ano, then the home router and the modem. A dropped name ends at Ano. An allowed name keeps going.
        </p>
      </div>
      <RouteMap cards={cards} speakerOnly={speakerOnly} onOpen={open} />
    </div>
  );
}

function DevicesPage({
  devices,
  cards,
  speakerOnly,
  open,
}: {
  devices: Device[];
  cards: Card[];
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
          Each row opens that device. On the next screen, open a name to see its charts and the AI Inference read. The rule buttons stay on the device and do not open a new screen.
        </p>
      </div>
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
                        <span className="mt-1 block text-[12px] text-muted-foreground">
                          {device.viaAno ? "Through Ano · " : ""}
                          {own.length === 0 ? "No names yet" : `${own.length} name${own.length === 1 ? "" : "s"} · ${protocolsOf(own)} · left ${bytes(left)} · kept ${bytes(kept)}`}
                        </span>
                        {speakerOnly && device.kind !== "speaker" ? (
                          <span className="mt-1 block text-[11px] text-muted-foreground">Hidden from Overview while Speakers only is on. Open still shows its names.</span>
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

function protocolsOf(cards: Card[]) {
  const names = [...new Set(cards.map((card) => card.protocol).filter((protocol): protocol is string => Boolean(protocol)))];
  return names.length === 0 ? "protocol unknown" : names.join(", ");
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

function devicePath(device: Device, via?: ViaPath) {
  const addr = via?.addr || "this computer";
  const port = via?.port || 10081;
  if (!device.viaAno) {
    return "Streaming stays on the direct path. Other names are monitored once this device sends them through Ano.";
  }
  if (device.kind === "tv" && device.name.toLowerCase().includes("fire tv")) {
    if (via?.steer) {
      return "This Fire TV reaches the network through this computer and can keep its DNS. Streaming stays direct. Other names are monitored. Return it to the router when you want the old path.";
    }
    return via?.steerDetail || "This Fire TV can keep its DNS. Bring it through Ano from this computer. Streaming stays direct. Other names are monitored.";
  }
  if (via?.dns) {
    return `Set the DNS server to ${addr}, or the HTTP proxy to ${addr} port ${port}. Streaming stays on the direct path.`;
  }
  return `Set the HTTP proxy to ${addr} port ${port}. ${via?.dnsDetail || ""}`;
}

function OpenChip({ label }: { label: string }) {
  return (
    <span className="inline-flex shrink-0 items-center gap-0.5 rounded-md border border-primary/30 bg-primary/10 px-2 py-1 text-[12px] font-medium text-primary">
      {label}
      <ChevronRight className="size-3.5" />
    </span>
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
  series,
  via,
  open,
  onBack,
  onDecide,
  onVia,
  onSteer,
}: {
  device: Device;
  cards: Card[];
  series: Sample[];
  via?: ViaPath;
  open: (id: string) => void;
  onBack: () => void;
  onDecide: (id: string, action: Choice) => void;
  onVia: (id: string, on: boolean) => void;
  onSteer: (on: boolean) => Promise<void>;
}) {
  const ids = new Set(cards.map((card) => card.id));
  const rows = sumLine(series.filter((sample) => ids.has(sample.patternId)));
  const [steerBusy, setSteerBusy] = useState(false);
  const fireTV = device.kind === "tv" && device.name.toLowerCase().includes("fire tv") && device.viaAno;
  return (
    <div className="space-y-4">
      <div>
        <BackLink onClick={onBack}>All devices</BackLink>
        <h1 className="mt-2 text-xl font-semibold tracking-tight">{device.name}</h1>
        <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">
          {device.room} · {kindTitle(device.kind)}. Add it to Ano to monitor names other than streaming. Open a name for its charts. The rule buttons stay on this screen.
        </p>
      </div>
      <section className="rounded-xl border bg-card px-4 py-3.5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <p className="text-sm font-medium">{device.viaAno ? "Through Ano" : "Add to Ano"}</p>
            <p className="mt-1 max-w-2xl text-[12px] leading-5 text-muted-foreground">{devicePath(device, via)}</p>
          </div>
          <button
            type="button"
            onClick={() => onVia(device.id, !device.viaAno)}
            className={
              device.viaAno
                ? "h-8 rounded-md border px-2.5 text-[12px] font-medium text-muted-foreground"
                : "h-8 rounded-md bg-primary px-2.5 text-[12px] font-medium text-primary-foreground"
            }
          >
            {device.viaAno ? "Remove" : "Add to Ano"}
          </button>
          {fireTV ? (
            <button
              type="button"
              disabled={steerBusy}
              onClick={() => {
                setSteerBusy(true);
                onSteer(!via?.steer).finally(() => setSteerBusy(false));
              }}
              className="h-8 rounded-md bg-primary px-2.5 text-[12px] font-medium text-primary-foreground disabled:opacity-60"
            >
              {steerBusy ? "Waiting for permission…" : via?.steer ? "Return to the router" : "Bring through Ano"}
            </button>
          ) : null}
        </div>
      </section>
      {cards.length === 0 ? <p className="text-sm text-muted-foreground">No names identified for this device yet.</p> : null}
      <div className="space-y-3">
        {cards.map((card) => (
          <section key={card.id} className="rounded-xl border bg-card p-4 shadow-[0_1px_2px_rgba(16,24,40,0.04)]">
            <button
              type="button"
              onClick={() => open(card.id)}
              className="flex w-full items-center gap-3 rounded-lg border border-transparent px-2 py-2 text-left hover:border-primary hover:bg-accent/50"
            >
              <span className="min-w-0 flex-1">
                <span className="block text-sm font-medium">{shortName(card)}</span>
                <span className="block text-[12px] text-muted-foreground">
                  {card.destination}
                  {card.protocol ? ` · ${card.protocol}` : ""}
                </span>
              </span>
              <OpenChip label="Open" />
            </button>
            {card.category === "seen" || card.category === "stream" ? (
              <p className="mt-3 border-t pt-3 text-[12px] leading-5 text-muted-foreground">{card.evidence}</p>
            ) : (
              <div className="mt-3 border-t pt-3">
                <p className="mb-2 text-[11px] font-medium tracking-wide text-muted-foreground uppercase">Rule for this name</p>
                <ChoicePanel card={card} onDecide={onDecide} />
              </div>
            )}
          </section>
        ))}
      </div>
      <div className="grid gap-4 xl:grid-cols-2">
        <Panel title="Bytes over time" hint="This device only">
          <BytesLine
            rows={rows}
            lines={[
              { key: "left", name: "Bytes left", color: "#FF9830" },
              { key: "kept", name: "Bytes kept", color: "#73BF69" },
            ]}
          />
        </Panel>
        <Panel title="Share of bytes that left">
          <SharePie slices={cards.map((card) => ({ name: shortName(card), value: card.metrics.beforeBytes }))} />
        </Panel>
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
      <ChoiceButton label="Refuse" meaning="Drop it. This name never reaches the router." active={current === "drop"} tone="drop" onClick={() => onDecide(card.id, "drop")} />
      <ChoiceButton label="Send" meaning="Let it go direct. It skips Ano and Envoy." active={current === "allow_direct"} tone="allow" onClick={() => onDecide(card.id, "allow_direct")} />
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

function PatternView({
  card,
  series,
  onDecide,
  onDevices,
  onDevice,
}: {
  card: Card;
  series: Sample[];
  onDecide: (id: string, action: Choice) => void;
  onDevices: () => void;
  onDevice: () => void;
}) {
  const rows = series.map((sample) => ({
    time: clock(sample.at),
    left: sample.leftBytes,
    kept: sample.keptBytes,
    lookups: sample.lookups,
    refused: sample.refused,
  }));
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
            {card.destination}
            {card.protocol ? ` · ${card.protocol}` : ""}
          </p>
        </div>
      </div>
      {card.category === "seen" || card.category === "stream" ? (
        <p className="max-w-3xl text-sm leading-6 text-muted-foreground">{card.evidence}</p>
      ) : (
        <ChoicePanel card={card} onDecide={onDecide} />
      )}
      <p className="max-w-3xl text-base leading-7">{plainSentence(card.sentence)}</p>
      {card.llmReason ? <QwenRead reason={card.llmReason} protocol={card.protocol} /> : null}
      <p className="max-w-3xl text-sm leading-6 text-muted-foreground">{card.consequence}</p>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Stat label="Lookups before" value={String(card.metrics.beforeLookups)} note={`About every ${card.metrics.everySeconds}s before the choice`} />
        <Stat label="Bytes before" value={bytes(card.metrics.beforeBytes)} note="What had already left, or is still leaving" />
        <Stat label="Lookups after" value={String(card.metrics.afterLookups)} note={card.metrics.dropped ? "Refused since the drop" : "Still zero until you drop it"} />
        <Stat label="Bytes after" value={bytes(card.metrics.afterBytes)} note={card.metrics.dropped ? "Kept on this machine" : "Nothing kept yet"} />
      </div>
      <div className="grid gap-4 xl:grid-cols-2">
        <Panel title="Bytes over time">
          <BytesLine
            rows={rows}
            lines={[
              { key: "left", name: "Bytes left", color: "#FF9830" },
              { key: "kept", name: "Bytes kept", color: "#73BF69" },
            ]}
          />
        </Panel>
        <Panel title="Lookups over time">
          <BytesLine
            rows={rows}
            lines={[
              { key: "lookups", name: "Lookups that left", color: "#5794F2" },
              { key: "refused", name: "Lookups refused", color: "#F2495C" },
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
          <p className="mt-3 text-sm leading-6">Suggested action: {card.suggestion}</p>
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
        <p className="mt-1 text-sm text-muted-foreground">Every drop, allow, and dismissal, newest first.</p>
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
  host,
  setHost,
  probe,
  onProbe,
}: {
  envoy: Envoy;
  cards: Card[];
  host: string;
  setHost: (value: string) => void;
  probe: string;
  onProbe: (event: FormEvent) => void;
}) {
  const active = cards.filter((card) => card.status === "approved");
  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Envoy</h1>
        <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">
          Dropped names are refused here. Allowed names go straight out and never enter Envoy. Only a route marked allow through Envoy is proxied.
        </p>
      </div>
      <div className="grid gap-3 sm:grid-cols-3">
        <Stat label="Process" value={envoy.running ? "Running" : "Down"} note={envoy.message || "No message"} />
        <Stat label="Listen" value={envoy.listen || "—"} note="Household traffic aimed at Ano" />
        <Stat label="Routes" value={String(envoy.routes)} note="Approved names Envoy knows" />
      </div>
      <Panel title="Approved names">
        {active.length === 0 ? (
          <p className="text-sm text-muted-foreground">Nothing is approved yet.</p>
        ) : (
          <ul className="space-y-2 text-sm">
            {active.map((card) => (
              <li key={card.id} className="flex flex-wrap items-baseline justify-between gap-2 border-b py-2 last:border-b-0">
                <span>
                  <span className="font-medium">{shortName(card)}</span>
                  <span className="ml-2 font-mono text-xs text-muted-foreground">{card.destination}</span>
                </span>
                <span>{card.action === "drop" ? "Drop" : card.action === "allow_envoy" ? "Through Envoy" : "Direct"}</span>
              </li>
            ))}
          </ul>
        )}
      </Panel>
      <Panel title="Try a name">
        <form onSubmit={onProbe} className="flex flex-col gap-3 sm:flex-row">
          <Input value={host} onChange={(event) => setHost(event.target.value)} aria-label="Hostname to check" className="sm:max-w-md" />
          <Button type="submit">Check through Envoy</Button>
        </form>
        {probe ? <p className="mt-3 text-sm leading-6">{probe}</p> : null}
      </Panel>
    </div>
  );
}
