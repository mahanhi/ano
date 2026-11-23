"use client";

import type { ReactNode } from "react";

import {
  CartesianGrid,
  Cell,
  Legend,
  Line,
  LineChart,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

export const palette = ["#FF9830", "#73BF69", "#5794F2", "#FADE2A", "#B877D9", "#F2495C"];

const tip = {
  contentStyle: {
    background: "#ffffff",
    border: "1px solid #e6e8ee",
    borderRadius: 10,
    color: "#1c2430",
    boxShadow: "0 8px 24px rgba(16, 24, 40, 0.08)",
  },
  labelStyle: { color: "#1c2430" },
};

export function Panel({
  title,
  hint,
  children,
  className = "",
  bodyClassName = "p-4",
}: {
  title: string;
  hint?: string;
  children: ReactNode;
  className?: string;
  bodyClassName?: string;
}) {
  return (
    <section className={`rounded-xl border bg-card shadow-[0_1px_2px_rgba(16,24,40,0.04)] ${className}`}>
      <header className="flex shrink-0 items-baseline justify-between gap-3 border-b px-4 py-2.5">
        <h2 className="text-[13px] font-medium text-foreground">{title}</h2>
        {hint ? <p className="truncate text-[12px] text-muted-foreground">{hint}</p> : null}
      </header>
      <div className={bodyClassName}>{children}</div>
    </section>
  );
}

function compactBytes(n: number) {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1)} GB`;
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)} MB`;
  if (n >= 1000) return `${Math.round(n / 1000)} KB`;
  return `${Math.round(n)} B`;
}

function byteScale(peak: number) {
  if (peak >= 1_000_000_000) return { div: 1_000_000_000, suffix: "GB" };
  if (peak >= 1_000_000) return { div: 1_000_000, suffix: "MB" };
  if (peak >= 1000) return { div: 1000, suffix: "KB" };
  return { div: 1, suffix: "B" };
}

export function BytesLine({
  rows,
  lines,
  unit = "bytes",
  className = "h-52 w-full",
}: {
  rows: Record<string, string | number>[];
  lines: { key: string; name: string; color: string }[];
  unit?: "bytes" | "count";
  className?: string;
}) {
  if (rows.length === 0) {
    return <p className={`grid place-items-center text-center text-sm text-muted-foreground ${className}`}>No history for this name yet.</p>;
  }
  const peak = Math.max(0, ...rows.flatMap((row) => lines.map((line) => Number(row[line.key]) || 0)));
  const scale = byteScale(peak);
  const format = (value: number) => {
    const n = Number(value);
    if (unit === "count") return n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(Math.round(n));
    const scaled = n / scale.div;
    const text = scaled >= 10 || scaled === 0 ? String(Math.round(scaled)) : scaled.toFixed(1);
    return `${text} ${scale.suffix}`;
  };
  return (
    <div className={className}>
      <ResponsiveContainer width="100%" height="100%" initialDimension={{ width: 640, height: 288 }}>
        <LineChart data={rows} margin={{ top: 22, right: 8, left: 0, bottom: 0 }}>
          <CartesianGrid stroke="rgba(15,23,42,0.06)" vertical={false} />
          <XAxis dataKey="time" stroke="#98a2b3" tick={{ fontSize: 11, fill: "#667085" }} minTickGap={48} axisLine={false} tickLine={false} />
          <YAxis
            stroke="#98a2b3"
            tick={{ fontSize: 11, fill: "#667085" }}
            width={52}
            tickFormatter={format}
            axisLine={false}
            tickLine={false}
          />
          <Tooltip {...tip} formatter={(value) => format(Number(value))} />
          <Legend verticalAlign="top" height={18} wrapperStyle={{ fontSize: 11, top: 0 }} />
          {lines.map((line) => (
            <Line
              key={line.key}
              type="monotone"
              dataKey={line.key}
              name={line.name}
              stroke={line.color}
              strokeWidth={2}
              dot={rows.length < 36}
              isAnimationActive={false}
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}

export function SharePie({
  slices,
  className = "h-72 w-full",
  legend = "bottom",
}: {
  slices: { name: string; value: number }[];
  className?: string;
  legend?: "bottom" | "side";
}) {
  const data = slices.filter((slice) => slice.value > 0);
  if (data.length === 0) {
    return <p className={`grid place-items-center text-center text-sm text-muted-foreground ${className}`}>Nothing to chart yet.</p>;
  }
  const pie = (
    <ResponsiveContainer width="100%" height="100%" initialDimension={{ width: 320, height: 220 }}>
      <PieChart>
        <Pie
          data={data}
          dataKey="value"
          nameKey="name"
          innerRadius={legend === "side" ? "48%" : 42}
          outerRadius={legend === "side" ? "78%" : 68}
          paddingAngle={2}
          stroke="#ffffff"
          isAnimationActive={false}
        >
          {data.map((slice, index) => (
            <Cell key={`${slice.name}-${index}`} fill={palette[index % palette.length]} />
          ))}
        </Pie>
        <Tooltip {...tip} formatter={(value) => compactBytes(Number(value))} />
        {legend === "bottom" ? <Legend wrapperStyle={{ fontSize: 12 }} /> : null}
      </PieChart>
    </ResponsiveContainer>
  );
  if (legend === "side") {
    return (
      <div className={`flex min-h-0 items-center gap-2 ${className}`}>
        <div className="h-full min-w-0 flex-1">{pie}</div>
        <ul className="max-h-full w-[48%] shrink-0 space-y-1 overflow-auto pr-1 text-[12px]">
          {data.map((slice, index) => (
            <li key={`${slice.name}-${index}`} className="flex items-center gap-1.5">
              <span className="size-2 shrink-0 rounded-full" style={{ background: palette[index % palette.length] }} />
              <span className="min-w-0 flex-1 truncate">{slice.name}</span>
              <span className="shrink-0 tabular-nums text-muted-foreground">{compactBytes(slice.value)}</span>
            </li>
          ))}
        </ul>
      </div>
    );
  }
  return <div className={`overflow-hidden ${className}`}>{pie}</div>;
}
