"use client";

import { useEffect, useState } from "react";

type Gate = {
  on: boolean;
  ready: boolean;
  canStart: boolean;
  detail: string;
  wan: string;
  wanAddr: string;
  lan: string;
  lanAddr: string;
  lanNet: string;
};

export function GatewayPanel() {
  const [gate, setGate] = useState<Gate | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function load() {
    const response = await fetch("/api/gate", { cache: "no-store" });
    if (!response.ok) throw new Error("Ano did not answer");
    setGate((await response.json()) as Gate);
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

  async function setOn(on: boolean) {
    setBusy(true);
    setError("");
    try {
      const response = await fetch("/api/gate", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ on }),
      });
      const body = (await response.json().catch(() => ({}))) as Gate & { error?: string };
      if (!response.ok) throw new Error(body.error || "The gateway did not change");
      setGate(body);
    } catch (err) {
      setError(err instanceof Error ? err.message : "The gateway did not change");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="rounded-xl border bg-card px-4 py-4">
      <h2 className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">Gateway</h2>
      <p className="mt-2 max-w-3xl text-sm leading-6 text-muted-foreground">
        Two Ethernet jacks. Router side faces the router. Access-point side faces Wi-Fi. Envoy carries traffic out. A refused endpoint is dropped.
      </p>
      <div className="mt-4 grid gap-2 sm:grid-cols-3">
        <Port kicker="Router side" title={gate?.wan || "—"} detail={gate?.wanAddr || "No port yet"} />
        <Port kicker="Ano" title="Records after Envoy" detail={gate?.lanAddr ? `Gateway ${gate.lanAddr}` : "Home gateway"} />
        <Port kicker="Access point side" title={gate?.lan || "—"} detail={gate?.lanNet || "No separate network yet"} />
      </div>
      <p className="mt-3 max-w-3xl text-[13px] leading-5 text-muted-foreground">{gate?.detail || "Reading the ports."}</p>
      {error ? <p className="mt-2 text-sm text-destructive">{error}</p> : null}
      <div className="mt-3">
        {gate?.on ? (
          <button type="button" disabled={busy} onClick={() => setOn(false)} className="h-9 rounded-md border px-3 text-sm font-medium disabled:opacity-60">
            {busy ? "Waiting for permission…" : "Stop the gateway"}
          </button>
        ) : (
          <button
            type="button"
            disabled={busy || !gate?.canStart}
            onClick={() => setOn(true)}
            className="h-9 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground disabled:opacity-60"
          >
            {busy ? "Waiting for permission…" : "Start the gateway"}
          </button>
        )}
      </div>
    </section>
  );
}

function Port({ kicker, title, detail }: { kicker: string; title: string; detail: string }) {
  return (
    <div className="rounded-lg border px-3 py-3">
      <p className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">{kicker}</p>
      <p className="mt-1 text-sm font-medium">{title}</p>
      <p className="mt-1 font-mono text-[12px] text-muted-foreground">{detail}</p>
    </div>
  );
}
