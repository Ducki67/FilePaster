# FilePaster

Simple LAN/RadminVPN file share exe.

Downloadable file types: `.rar`, `.7z`, `.zip`, `.torrent`.

Description text: edit `shared/description.txt` and it shows on the page for everyone.

## Build

Windows:

```bash
./build-windows.ps1
```

This uses `icon.ico` in the project root and embeds it into `FilePaster.exe`.

Linux:

```bash
go build -o FilePaster .
```

Cross-build Linux binaries from Windows:

```bash
./build-linux.ps1
```

## Use the exe

1. Run the exe once.
2. It creates `shared/` and `filepaster.config.json` next to the exe.
3. Edit `filepaster.config.json`:
- `owner_credit`: your name shown on the page.
- `advertise_host`: your RadminVPN IP (usually starts with `26.`).
- `peer_hosts`: optional friend IPs.
- `password`: optional lock.
4. Put zip/rar/files inside `shared/`.
5. Edit `shared/description.txt` with notes/how-to for users.
6. Run exe and send the shown URL.

## Example config

```json
{
  "port": "8080",
  "bind_address": "0.0.0.0",
  "advertise_host": "26.62.15.179",
  "share_folder": "shared",
  "password": "",
  "peer_hosts": ["26.11.22.33"],
  "owner_credit": "YourName"
}
```

## Linux friend test
NOTE: This was Not tested before so if there's any issue please let me know in the **issue** tickets!!


1. Build Linux binaries on Windows with `./build-linux.ps1`.
2. Send `FilePaster-linux-amd64` to your friend (or `FilePaster-linux-arm64` for ARM).
3. On Linux, run:

```bash
chmod +x FilePaster-linux-amd64
./FilePaster-linux-amd64
```

4. Put share files in `shared/` next to the binary and open the shown URL.
