# Ano

Ano is the on-machine agent. It collects device metadata into SQLite, asks AI Inference for drop or allow, and the screen reads that API. Passive mode is the default and does not enforce. Active mode does. The household plan, including the later Raspberry Pi, is in [Ano.md](Ano.md).

This proof of concept does not need a Pi. The Go advisor, the policy store, and Envoy run here. On a Mac the same commands work after `brew install go node envoy`.

## Run

In one terminal:

```bash
# Linux: download the Envoy binary once
bash scripts/install-envoy.sh

# macOS: brew install envoy
# local model: ollama pull qwen2.5:0.5b && ollama serve
go run ./cmd/hbar
```

In another:

```bash
cd web
npm install
npm run dev
```

Open [Ano](http://127.0.0.1:4317). The Route page draws the path from the kitchen speaker through Wi-Fi, Ano, the home router, and the modem. Dots move along that path. A dropped name stops inside Ano. A name that is still allowed continues to the internet.

The screen is a dashboard. The menu on the left opens the overview, the route, the system design, each speaker, each destination, the decision log, and Envoy. Overview and each name show a line of bytes that left versus bytes kept, plus pie charts of the share. The counts move every two seconds. Speakers only is on until you turn it off at the bottom of the menu.

Nothing is enforced until you open a name and choose Drop it, Let it go direct, or Not a leak. Then use Check through Envoy on the Envoy page.

- Review screen: port 4317
- Go processor: port 8647
- Envoy: port 10080
- Envoy admin, local only: port 19001

Direct allows are absent from Envoy on purpose. A drop answers 403. An allow through Envoy answers 200. On the future inline gateway, drops move to nftables and Envoy keeps only proxy allows. That split is described in Ano.md.

## Tests

```bash
go test ./internal/... ./cmd/...
```
