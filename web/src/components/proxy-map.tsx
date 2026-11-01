"use client";

import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

type ProxyCard = {
  id: string;
  deviceName: string;
  room: string;
  destination: string;
  activity: string;
  status: string;
  action: string;
  reroute?: string;
};

const names: Record<string, string> = {
  "device-metrics-us.amazon.com": "Metrics US",
  "device-metrics-us-2.amazon.com": "Metrics US 2",
  "avs-alexa-na.amazon.com": "Voice answers",
  "app-measurement.com": "Ad measurement",
};

function labelOf(card: ProxyCard) {
  return names[card.destination] ?? card.activity;
}

function throughProxy(card: ProxyCard) {
  return card.status === "approved" && card.action === "allow_envoy";
}

export function ProxyMap() {
  const [cards, setCards] = useState<ProxyCard[]>([]);
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState("");
  const [note, setNote] = useState("");
  const [error, setError] = useState("");

  async function load() {
    const response = await fetch("/api/state?all=1", { cache: "no-store" });
    if (!response.ok) throw new Error("Ano did not answer");
    const body = (await response.json()) as { cards?: ProxyCard[] };
    const next = body.cards ?? [];
    setCards(next);
    setDrafts((current) => {
      const draftsNext = { ...current };
      for (const card of next) {
        draftsNext[card.id] = card.reroute ?? "";
      }
      return draftsNext;
    });
  }

  useEffect(() => {
    load().catch((err: Error) => setError(err.message));
  }, []);

  async function save(card: ProxyCard, host: string) {
    setBusy(card.id);
    setNote("");
    setError("");
    try {
      const response = await fetch(`/api/cards/${card.id}/reroute`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ host }),
      });
      if (!response.ok) {
        const body = (await response.json().catch(() => ({}))) as { error?: string };
        throw new Error(body.error || "That map did not save");
      }
      await load();
      setNote(host ? `${labelOf(card)} now re-routes to ${host}.` : `${labelOf(card)} answers on this machine again.`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "That map did not save");
    } finally {
      setBusy("");
    }
  }

  return (
    <div className="space-y-5">
      <div>
        <h1 className="text-[22px] font-semibold tracking-tight">Proxy</h1>
        <p className="mt-1 max-w-3xl text-[13px] leading-5 text-muted-foreground">
          Map where Envoy sends a name. Saving puts that name through the proxy and rewrites the host to the one you enter, on port 443. Clear the target to keep Answer here on this machine.
        </p>
      </div>
      {error ? <p className="rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm">{error}</p> : null}
      {note ? <p className="rounded-lg border bg-card px-3 py-2 text-sm">{note}</p> : null}
      {cards.length === 0 ? <p className="text-sm text-muted-foreground">No names are on the list yet.</p> : null}
      <ul className="space-y-3">
        {cards.map((card) => {
          const draft = drafts[card.id] ?? "";
          const mapped = throughProxy(card) && card.reroute;
          return (
            <li key={card.id} className="rounded-xl border bg-card p-4 shadow-[0_1px_2px_rgba(16,24,40,0.04)]">
              <div className="flex flex-wrap items-baseline justify-between gap-2">
                <div>
                  <p className="text-sm font-medium">{labelOf(card)}</p>
                  <p className="text-xs text-muted-foreground">
                    {card.deviceName} · {card.room}
                  </p>
                </div>
                <p className="text-xs text-muted-foreground">{mapped ? `Re-routes to ${card.reroute}` : throughProxy(card) ? "Answers on this machine" : "Not through the proxy yet"}</p>
              </div>
              <div className="mt-4 flex flex-col gap-2 lg:flex-row lg:items-center">
                <span className="min-w-0 truncate rounded-lg border bg-muted px-3 py-2 font-mono text-[12px] lg:max-w-xs lg:flex-1">{card.destination}</span>
                <span className="text-center text-muted-foreground lg:px-1" aria-hidden>
                  →
                </span>
                <span className="rounded-lg border border-primary/30 bg-accent px-3 py-2 text-center text-[13px] font-medium text-accent-foreground">Envoy</span>
                <span className="text-center text-muted-foreground lg:px-1" aria-hidden>
                  →
                </span>
                <Input
                  value={draft}
                  onChange={(event) => setDrafts((current) => ({ ...current, [card.id]: event.target.value }))}
                  aria-label={`Re-route target for ${labelOf(card)}`}
                  placeholder="logs.example.net"
                  className="lg:max-w-xs lg:flex-1"
                />
                <div className="flex gap-2">
                  <Button type="button" size="sm" disabled={busy === card.id || draft.trim() === ""} onClick={() => save(card, draft.trim())}>
                    Map it
                  </Button>
                  <Button type="button" size="sm" variant="outline" disabled={busy === card.id || !card.reroute} onClick={() => save(card, "")}>
                    Clear
                  </Button>
                </div>
              </div>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
