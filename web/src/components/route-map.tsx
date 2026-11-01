"use client";

export type RouteCard = {
  id: string;
  deviceName: string;
  kind: string;
  destination: string;
  activity: string;
  status: string;
  action: string;
  metrics: { dropped: boolean };
};

const names: Record<string, string> = {
  "device-metrics-us.amazon.com": "Metrics US",
  "device-metrics-us-2.amazon.com": "Metrics US 2",
  "avs-alexa-na.amazon.com": "Voice answers",
  "app-measurement.com": "Ad measurement",
};

function labelOf(card: Pick<RouteCard, "destination" | "activity">) {
  return names[card.destination] ?? card.activity;
}

type Lane = "drop" | "direct" | "envoy";

function laneOf(card: RouteCard): Lane {
  if (card.metrics.dropped) return "drop";
  if (card.status === "approved" && card.action === "allow_envoy") return "envoy";
  return "direct";
}

function colorOf(card: RouteCard) {
  if (card.metrics.dropped) return "#F2495C";
  if (card.status === "approved" && card.action === "allow_envoy") return "#5794F2";
  if (card.status === "approved") return "#73BF69";
  return "#FF9830";
}

function outcomeOf(card: RouteCard) {
  if (card.metrics.dropped) return "Stops at Ano";
  if (card.status === "approved" && card.action === "allow_envoy") return "Through Envoy";
  if (card.status === "approved") return "Straight out";
  return "Still leaving";
}

export function RouteMap({
  cards,
  speakerOnly,
  onOpen,
  compact = false,
}: {
  cards: RouteCard[];
  speakerOnly: boolean;
  onOpen: (id: string) => void;
  compact?: boolean;
}) {
  const dropped = cards.filter((card) => laneOf(card) === "drop");
  const direct = cards.filter((card) => laneOf(card) === "direct");
  const envoy = cards.filter((card) => laneOf(card) === "envoy");
  const inbound = uniqueColors([...dropped, ...direct, ...envoy]);
  const outbound = uniqueColors([...direct, ...envoy]);
  const directColor = direct.some((card) => card.status !== "approved") ? "#FF9830" : "#73BF69";

  return (
    <div className="space-y-3">
      {compact ? null : (
        <p className="max-w-2xl text-[13px] leading-5 text-muted-foreground">
          Red stops inside Ano. Orange is still undecided and leaves. Green was allowed straight out. Blue goes through Envoy.
        </p>
      )}
      <div className="route-stage w-full rounded-xl border bg-muted/60 px-4 py-4 sm:px-5 sm:py-5">
        <div className="route-board">
          <Pill title="Speaker" detail="Kitchen" accent="#73BF69" />
          <Flow colors={inbound} />
          <Pill title="Wi-Fi" detail="Access point" />
          <Flow colors={inbound} />
          <div className="route-hbar min-w-0 rounded-xl border border-primary/30 bg-card px-3 py-2.5">
            <p className="px-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">Ano</p>
            <Lane title="Drop" detail={namesOn(dropped, "None")} color="#F2495C" stop={dropped.length > 0} />
            <Lane title="Out" detail={namesOn(direct, "None")} color={direct.length ? directColor : "#5c6770"} />
            <Lane title="Envoy" detail={namesOn(envoy, "None")} color={envoy.length ? "#5794F2" : "#5c6770"} />
          </div>
          <Flow colors={outbound} />
          <Pill title="Router" detail="Home" />
          <Flow colors={outbound} />
          <Pill title="Modem" />
          <Flow colors={outbound} />
          <Pill title="Internet" detail="Outside" />
        </div>
        <p className="mt-4 text-xs leading-5 text-muted-foreground">
          TV and thermostat share this Wi-Fi. {speakerOnly ? "The hall phone is not watched." : "The hall phone is included."}
        </p>
      </div>
      {compact ? null : (
        <ul className="grid gap-1.5 sm:grid-cols-2 xl:grid-cols-3">
          {cards.map((card) => (
            <li key={card.id}>
              <button
                type="button"
                onClick={() => onOpen(card.id)}
                className="flex w-full items-center gap-2 rounded-lg border bg-card px-2.5 py-2 text-left shadow-[0_1px_2px_rgba(16,24,40,0.04)] hover:border-primary hover:bg-accent/50"
              >
                <span className="size-1.5 shrink-0 rounded-full" style={{ background: colorOf(card) }} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] font-medium">{labelOf(card)}</span>
                  <span className="block truncate text-[11px] text-muted-foreground">
                    {card.deviceName} · {outcomeOf(card)}
                  </span>
                </span>
                <span className="shrink-0 text-[12px] font-medium text-primary">Open</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function uniqueColors(cards: RouteCard[]) {
  return [...new Set(cards.map(colorOf))].slice(0, 3);
}

function namesOn(cards: RouteCard[], empty: string) {
  if (cards.length === 0) return empty;
  return cards.map(labelOf).join(", ");
}

function Pill({ title, detail, accent = "#8b95a3" }: { title: string; detail?: string; accent?: string }) {
  return (
    <div className="route-pill rounded-xl border bg-card px-3.5 py-2.5" style={{ borderColor: accent }}>
      <p className="text-sm leading-5 font-medium whitespace-nowrap @min-[820px]:text-base">{title}</p>
      {detail ? <p className="text-xs leading-4 text-muted-foreground">{detail}</p> : null}
    </div>
  );
}

function Lane({ title, detail, color, stop = false }: { title: string; detail: string; color: string; stop?: boolean }) {
  return (
    <div className="mt-1.5 flex items-center gap-2 rounded-lg border bg-background px-2.5 py-1.5">
      <span className="size-2 shrink-0 rounded-full" style={{ background: color }} />
      <span className="w-12 shrink-0 text-[13px] font-medium" style={{ color }}>
        {title}
      </span>
      <span className="min-w-0 flex-1 text-[13px] leading-4 text-foreground/80">{detail}</span>
      {stop ? <span className="text-sm leading-none font-medium text-[#F2495C]">×</span> : null}
    </div>
  );
}

function Flow({ colors }: { colors: string[] }) {
  return (
    <div className="route-link">
      {colors.map((color, index) => (
        <span key={color} className="route-dot" style={{ background: color, animationDelay: `${index * 0.45}s` }} />
      ))}
    </div>
  );
}
