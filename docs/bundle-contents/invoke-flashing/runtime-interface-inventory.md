# Runtime Interface Inventory

Static inventory from the extracted `83_IMAGE` rootfs. This is an interface
map, not proof that any service is safe to expose on a modern network.

| Boundary | Evidence | Current interpretation |
|---|---|---|
| MCU control | `mcu-interface 127.0.0.1 9999` | Local TCP-style service boundary; wire protocol unknown |
| Audio/UI | `audio-ui 127.0.0.1 9999 $boot_mode $run_mode` | Shares the local service endpoint with MCU-facing components |
| DSP | `dsp-client` | Local process boundary; transport details unknown |
| Network services | Firewall rules for 22, 9998, 9999, HTTPS, UDP 48301, DHCP, mDNS | Recovery/setup and maintenance surface; authentication semantics unresolved |
| Process supervision | `system-manager /etc/podium/podium.conf` | Podium starts and restarts MCU, audio, connection, music, Bluetooth, Cortana, and OTA processes |
| OTA | `ota_rbua_install.sh` and RedBend RB_UA config | Separate update orchestration targeting `bootimgs` and `rootfs` |
| Service Ethernet | `serviceport.sh` | Configures `eth0`, defaulting to `172.20.20.20`, and announces it with gratuitous ARP |
| Bluetooth | `bluetooth.sh`, `btmrvl.ko`, `sd8887` firmware | Marvell SDIO Bluetooth path; exact host-controller wiring remains unresolved |
| Wi-Fi | `mlan.ko`, `sd8xxx.ko`, `sd8887_wlan_a2_p78.bin` | Marvell SDIO Wi-Fi path with LS9 calibration profiles |
| Cortana | `cortana.sh`, `cortana-harness` | Voice-assistant process boundary; cloud/backend dependencies unknown |

The strongest near-term revival target is the local audio/UI/MCU boundary:
determine whether the original compute module can be made to perform local
playback and controls without reproducing the cloud assistant. That requires
read-only runtime observation or bench-level protocol capture; static files
alone do not establish the command format.
