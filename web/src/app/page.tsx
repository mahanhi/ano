import { Home, type DashboardState } from "@/components/home";

export const dynamic = "force-dynamic";

export default async function Page() {
  try {
    const response = await fetch("http://127.0.0.1:8647/api/state", { cache: "no-store" });
    if (!response.ok) throw new Error(String(response.status));
    const state = (await response.json()) as DashboardState;
    return <Home initialState={{ ...state, series: state.series ?? [] }} />;
  } catch {
    return (
      <main className="grid min-h-screen place-items-center bg-background px-6 text-center text-foreground">
        <div className="max-w-md">
          <p className="text-base font-medium">Ano did not answer</p>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">
            The screen is up, but the control process on port 8647 did not return the dashboard. Refresh once that process is running.
          </p>
        </div>
      </main>
    );
  }
}
