# Ano

Ano is a home privacy appliance. It records where each device sends, asks a local model whether that destination should be refused or sent, and enforces only after a person or Active mode says so. The model stays off the packet path. Ano stores metadata, and it does not store payloads.

The screen, the schedule, and Ask Ano run on this computer and on the Orange Pi. The gateway, which forwards the home network, runs on the Orange Pi after a second Ethernet jack is connected. This Mac is a development machine on the home network. It is not the house gateway.

- [Requirements](REQUIREMENTS.md) is what the running app must do.
- [System design](SYSTEM-DESIGN.md) is how the household uses Ano.
- [Architecture](ARCHITECTURE.md) is how the programs and the packet path fit together.
- [Ano.md](Ano.md) is the earlier plan.

## What the screen does

The menu is Overview, Route, System, Devices, Admin, Decisions, Envoy, and Proxy. Ask Ano sits on every screen. Passive and Active sit in the menu. Speakers only stays on until it is turned off.

Passive mode records devices and stores the model's suggestion. Active mode enforces Refuse and Send. A person's button overrides the model.

Open a name and choose:

- **Refuse.** Drop that name. On this computer, Envoy answers 403. On the Orange Pi, with the gateway on, DNS answers NXDOMAIN and the kernel drops the address.
- **Send.** Let it go direct.
- **Answer here.** Envoy answers on Ano, or Proxy sends that name to another host. A local answer does not call the vendor.
- **Set aside.** Leave it unenforced.

Admin sets the review schedule and the DNS drop list. The schedule can be every minute, hour, day, week, or month. The default is every day at 03:00. Each review reads the past 24 hours.

Ask Ano takes a typed or spoken request. A complete request changes the mode, the schedule, a drop, a device choice, Speakers only, or the gateway. A greeting or a general question gets an answer. A half-finished request gets one question and the choices that fit.

## This computer

Install Go, Node, and Envoy, then run the API and the screen:

```bash
brew install go node envoy
# local model: ollama pull qwen2.5:0.5b && ollama serve
ANO_ENVOY_BIN=/opt/homebrew/bin/envoy go run ./cmd/ano
```

```bash
cd web
npm install
npm run dev
```

Open [Ano](http://127.0.0.1:4317).

The Mac model is Ollama with `qwen2.5:0.5b`. Override it with `ANO_LLM_URL`, `ANO_LLM_MODEL`, and `ANO_LLM_KIND`. The model runs on the schedule and when Ask Ano is used. It does not see each packet.

Starting the gateway on this Mac is refused, and IP forwarding stays off. The System page leaves Start off.

## Orange Pi 6 Plus

The always-on box is an Orange Pi 6 Plus: a 12-core ARM64 board with two RTL8126 5-gigabit Ethernet ports. Use the official Debian or Ubuntu image so both ports come up. Do not install Raspberry Pi OS. Linux names the ports from PCI addresses, so label them from `ip -4 addr` after the first boot.

Wiring, once both jacks are connected:

```text
modem or existing router -- router-side jack
Ano
access-point-side jack -- Wi-Fi access point -- TVs, speakers, phones
```

Put the access point in access-point mode. Its uplink goes to the access-point jack. The other jack goes to the current router.

Build the arm64 binary on the Mac, copy the repository to the board, and run the installer there as root:

```bash
GOOS=linux GOARCH=arm64 go build -o ano ./cmd/ano
sh deploy/orangepi/install.sh
```

The installer adds nftables, conntrack, dnsmasq, and Envoy 1.40.2. It installs Ollama, pulls Llama 3.2 1B, builds the screen, and enables Ano, the screen, and the model. Those three start on boot and come back if they stop. Envoy is started by Ano. dnsmasq stays disabled until the gateway is turned on. The installer does not start the gateway.

The screen listens on port 80. On the Pi, open [http://ano.home.security.ai/](http://ano.home.security.ai/). That name is written in the Pi's `/etc/hosts`, and Ano's DNS answers it for the home network when the gateway is on. This Mac does not use that name. Start the gateway from Admin only after the second Ethernet jack is up. With one network, Start stays disabled. The on-board NPU is not used.

The board is powered by USB-C Power Delivery at 20 V. It is specified for a 100 W Type-C PD supply.

## Ports

| Service | Port |
| --- | --- |
| Review screen on Ano | 80 |
| Review screen on this Mac, during development | 4317 |
| Ano API | 8647 |
| Envoy name check | 10080 |
| Envoy TCP egress, including HTTPS | 10082 |
| Envoy UDP egress, including QUIC | 10083 |
| Envoy admin | 19001, localhost only |
| Added-device HTTP proxy | 10081 |
| DNS relay for that proxy path | 10053 |
| Ollama | 11434, localhost only |

SQLite is `data/ano.db` on this computer and `/var/lib/ano/ano.db` on the Orange Pi. Ano is the only writer.

## Tests

```bash
go test ./internal/... ./cmd/...
```
