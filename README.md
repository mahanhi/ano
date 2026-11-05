# Ano

Ano is a home privacy appliance. It sits between the household and the internet, records where each device is sending, and asks AI Inference whether that destination should be refused or sent. A person decides. The model is not on the packet path, and Ano does not store payloads.

This repository is the proof of concept on the computer you are using, plus the gateway that runs on an Orange Pi 6 Plus. The longer design note is [Ano.md](Ano.md).

## What the screen does

Passive mode is the default. Ano records devices and stores the AI Inference suggestion. It does not enforce that suggestion. Active mode enforces **Refuse** and **Send**. A person's button overrides the model.

Open a name and choose:

- **Refuse.** Drop it. The name does not reach the router. Envoy answers 403 on this computer. On the Orange Pi, DNS answers NXDOMAIN and nftables drops the address.
- **Send.** Let it go direct. It skips Ano and Envoy.
- **Answer here.** This machine answers through Envoy. It does not call the vendor.
- **Set aside.** Not a leak. Nothing is enforced.

The menu opens the overview, the route, the system page, each device, the decision log, Envoy, and the proxy map. Overview and each name show bytes that left versus bytes kept. Those counts move every two seconds. Speakers only stays on until you turn it off at the bottom of the menu.

A device line shows its type, IP address, and MAC address. Traffic shows the endpoint and port the packets are leaving to.

## This computer

This Mac is a development machine. It is a peer on the home network, not the gateway. The System page names the two ports it can see and leaves **Start the gateway** off. Starting it here is refused, and IP forwarding stays off.

Outgoing packets from the televisions stay on the existing router. Ano on this Mac records conversations this computer already has with a device, such as Cast, AirPlay, and Sonos.

## Run

On a Mac, install Go, Node, and Envoy first: `brew install go node envoy`.

In one terminal:

```bash
# Linux: download the Envoy binary once
bash scripts/install-envoy.sh

# macOS: brew install envoy
# local model: ollama pull qwen2.5:0.5b && ollama serve
HBAR_ENVOY_BIN=/opt/homebrew/bin/envoy go run ./cmd/hbar
```

In another:

```bash
cd web
npm install
npm run dev
```

Open [Ano](http://127.0.0.1:4317).

The local model is Ollama with `qwen2.5:0.5b`. Override it with `HBAR_QWEN_URL` and `HBAR_QWEN_MODEL`. It runs when a card is created and returns a suggestion. It does not see each packet.

## Orange Pi 6 Plus

The always-on box is an Orange Pi 6 Plus: a 12-core ARM64 board with two RTL8126 5-gigabit Ethernet ports. Use the official Debian or Ubuntu image so both ports come up. Do not install Raspberry Pi OS.

Wiring:

```text
modem or existing router  --  router-side jack
Orange Pi running Ano
access-point-side jack  --  Wi-Fi access point  --  TVs, speakers, phones
```

Put the Nest, or another access point, in access-point mode only. Its uplink cable goes to the access-point jack. The other jack goes to the current router. After the first boot, run `ip -4 addr` and label the jacks. Linux names them from PCI addresses, not from a fixed `eth0`.

On the Pi, Ano forwards the home network out the router-side port, records each conversation from conntrack, and hands out DHCP and DNS with dnsmasq. Devices receive the Pi as their gateway and as their only DNS server. Streaming video and music stay forwarded. An approved refuse is NXDOMAIN plus an nftables drop, because televisions often skip DNS.

Build the arm64 binary on this Mac, copy it to the Pi with `deploy/orangepi/`, then run the installer there as root:

```bash
GOOS=linux GOARCH=arm64 go build -o hbar ./cmd/hbar
```

```bash
sh deploy/orangepi/install.sh
```

The installer adds nftables, conntrack, and dnsmasq, and enables `deploy/orangepi/ano.service`. Open the System page on the Pi and press **Start the gateway**. The on-board NPU is not used. The text model stays on the CPU.

The board is powered by USB-C Power Delivery at 20 V. It is specified for a 100 W Type-C PD supply.

## Ports

- Review screen: 4317
- Go processor: 8647
- Envoy: 10080
- Envoy admin, local only: 19001
- HTTP proxy for an added device: 10081
- DNS relay: 10053

SQLite lives at `data/ano.db` on this computer and `/var/lib/ano` on the Orange Pi. Go is the only writer.

## Tests

```bash
go test ./internal/... ./cmd/...
```
