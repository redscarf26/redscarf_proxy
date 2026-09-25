# redscarf

Local proxy for playing World of Warcraft Wrath of the Lich King Classic with the 3.4.3.54261 client on a 3.3.5a realm such as AzerothCore.

The 3.4.3 client speaks a different protocol from a 3.3.5a server. redscarf sits on the same PC as the client, accepts the modern Battle.net and world connection, translates it, and connects to the older auth server and world server you configure. It is a client-side bridge, not a server emulator. You supply the 3.4.3.54261 game client, Arctium WoW Launcher, and a running 3.3.5a realm.

One program does the startup. `redscarf.exe` answers the local version check, points the client at itself, runs the protocol proxy, and opens the game through Arctium.

## Download

GitHub builds a Windows zip on every push to `main` and attaches it to the [latest release](https://github.com/redscarf26/redscarf_proxy/releases/tag/latest). `redscarf-windows.zip` contains:

- `redscarf.exe`
- `Arctium WoW Launcher.exe`, from [arctium-wow-launcher](https://github.com/redscarf26/arctium-wow-launcher/releases)
- `config.ini`
- `open-second-client.cmd`

Unzip the folder, set `wow_path` in `config.ini`, then run `redscarf.exe`. The same release also has `redscarf.exe` by itself.

## Requirements

- `WowClassic.exe` from client build 3.4.3.54261
- `Arctium WoW Launcher.exe`
- A 3.3.5a auth server, usually AzerothCore on port 3724
- `redscarf.exe` built from this repository

## Build

From the repository root:

```bat
go build -trimpath -ldflags "-s -w" -o redscarf.exe .
```

## Configure

Copy `config.ini.example` to `config.ini` and set:

- `wow_path`: the `_classic_` directory, or the full path to `WowClassic.exe`
- `server_ip` and `server_port`: the 3.3.5a auth server, usually port `3724`
- `arctium_path`: optional. Defaults to `Arctium WoW Launcher.exe` in the same directory as `redscarf.exe`

Keep `config.ini` on the local machine. Do not commit it.

## Start

Run `redscarf.exe`. Log in after the window says you can log in. Leave that window open while playing. Closing it stops the proxy and disconnects the client.

The launcher points Arctium's version check at `127.0.0.1:8080`, writes `portal` in `WTF\Config.wtf` as `127.0.0.1:7000`, listens there for the client, and connects onward to `server_ip:server_port`.

## Second window

Leave the first launcher window open. When the first client reaches the login screen, run `open-second-client.cmd`. It opens another client through the proxy that is already running.
