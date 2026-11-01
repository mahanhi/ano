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

export function Panel({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return (
    <section className="rounded-xl border bg-card shadow-[0_1px_2px_rgba(16,24,40,0.04)]">
      <header className="flex items-baseline justify-between gap-3 border-b px-4 py-3">
        <h2 className="text-[13px] font-medium text-foreground">{title}</h2>
        {hint ? <p className="text-[12px] text-muted-foreground">{hint}</p> : null}
      </header>
      <div className="p-4">{children}</div>
    </section>
  );
}

export function BytesLine({
  rows,
  lines,
  unit = "bytes",
}: {
  rows: Record<string, string | number>[];
  lines: { key: string; name: string; color: string }[];
  unit?: "bytes" | "count";
}) {
  if (rows.length < 2) {
    return <p className="py-16 text-center text-sm text-muted-foreground">Waiting for the next live samples.</p>;
  }
  const format =
    unit === "count"
      ? (value: number) => (value >= 1000 ? `${(value / 1000).toFixed(1)}k` : String(Math.round(value)))
      : (value: number) => (value >= 1_000_000 ? `${(value / 1_000_000).toFixed(1)} MB` : value >= 1000 ? `${Math.round(value / 1000)} KB` : `${value} B`);
  return (
    <div className="h-52 w-full">
      <ResponsiveContainer width="100%" height="100%" initialDimension={{ width: 640, height: 288 }}>
        <LineChart data={rows} margin={{ top: 8, right: 12, left: 0, bottom: 0 }}>
          <CartesianGrid stroke="rgba(15,23,42,0.06)" vertical={false} />
          <XAxis dataKey="time" stroke="#98a2b3" tick={{ fontSize: 12, fill: "#667085" }} minTickGap={28} axisLine={false} tickLine={false} />
          <YAxis
            stroke="#98a2b3"
            tick={{ fontSize: 12, fill: "#667085" }}
            width={72}
            tickFormatter={format}
            axisLine={false}
            tickLine={false}
          />
          <Tooltip {...tip} />
          <Legend wrapperStyle={{ fontSize: 13 }} />
          {lines.map((line) => (
            <Line
              key={line.key}
              type="monotone"
              dataKey={line.key}
              name={line.name}
              stroke={line.color}
              strokeWidth={2}
              dot={false}
              isAnimationActive={false}
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}

export function SharePie({ slices }: { slices: { name: string; value: number }[] }) {
  const data = slices.filter((slice) => slice.value > 0);
  if (data.length === 0) {
    return <p className="py-16 text-center text-sm text-muted-foreground">Nothing to chart yet.</p>;
  }
  return (
    <div className="h-52 w-full">
      <ResponsiveContainer width="100%" height="100%" initialDimension={{ width: 640, height: 288 }}>
        <PieChart>
          <Pie
            data={data}
            dataKey="value"
            nameKey="name"
            innerRadius={42}
            outerRadius={68}
            paddingAngle={2}
            stroke="#ffffff"
            isAnimationActive={false}
          >
            {data.map((slice, index) => (
              <Cell key={slice.name} fill={palette[index % palette.length]} />
            ))}
          </Pie>
          <Tooltip {...tip} />
          <Legend wrapperStyle={{ fontSize: 13 }} />
        </PieChart>
      </ResponsiveContainer>
    </div>
  );
}
