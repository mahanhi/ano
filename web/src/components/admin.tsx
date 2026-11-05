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
            Add a device to send it through Ano. Streaming video and music stay on the direct path. Every other name is recorded here. Passive mode watches those names. Active mode applies Refuse and Send.
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
    return device.viaAno ? "Added. Its own packets still go to the router." : "No bytes seen from this computer yet";
  }
  return `${device.names} name${device.names === 1 ? "" : "s"} · seen ${bytes(device.bytesLeft)} · kept ${bytes(device.bytesKept)}`;
}

function addedHint(device: AdminDevice, admin: AdminView | null) {
  if (!device.viaAno) {
    return "Ano records conversations this computer already has with this device.";
  }
  if (admin?.steerOn) {
    return "A redirect is still on. Return added devices to the router.";
  }
  return "Added. Its own packets still go to the router. That redirect stays off.";
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
        {admin?.steerDetail || "Outgoing packets stay on the router. Moving them through this Mac restarted it, so that path stays off."}{" "}
        {admin?.viaAddr ? `${added} device${added === 1 ? "" : "s"} added.` : "Ano is waiting for this computer’s Wi-Fi address."}
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
