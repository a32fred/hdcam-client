# Discovery and recovery after reset

The service reconnects using the configured camera UID. It never resets the camera or changes Wi-Fi settings. A reboot or temporary network outage can recover once the existing credentials work again; factory reset can erase them.

## Local discovery

```sh
bin/hdcam discover --interface CAMERA_LAN_INTERFACE --duration 3s
```

Discovery explicitly binds the selected Linux interface and its sole eligible private IPv4 address. It sends up to three discovery requests to that subnet's directed broadcast, UDP8600, then returns candidate UID/MAC/IP records. Interfaces must be up, non-loopback, with one RFC1918 IPv4 prefix between /16 and /30. Multiple eligible addresses are rejected rather than selected implicitly. UDP6801–6803 must have a free bind port; setting the interface socket option can require OS capabilities. No automatic sudo is used. Replies are untrusted discovery data, not cryptographic proof of camera identity. Results contain private device identifiers; do not post them publicly.

The hardware-checked 462-byte response uses a 24-byte UID field at offset92 and MAC at84. Other accepted response lengths use the app-observed 32-byte UID field at124; those variants remain provisional. A plausible printable field alone is not confirmation that the selected physical camera was found.

## Preparing credentials without sending

Copy `examples/wifi.example.json` outside the checkout, replace the fields locally and set mode 0600. Preserve significant spaces in passwords.

```sh
bin/hdcam wifi-plan --config /private/path/camera.json --wifi-profile /private/path/wifi.json
```

This validates the private profiles and constructs an in-memory provision request. Output contains only packet metadata and readiness limits; it does not contain the UID, SSID, password or raw packet. **There is no sending option.** No Wi-Fi association, reset, broadcast, settings change or persistence is performed by this command.

The observed app AP protocol uses a 174-byte UDP request to port8600 with DID, SSID and password fields. This path sends credentials in plaintext and has no demonstrated cryptographic authentication. The current serializer deliberately supports printable ASCII only; a normal SSID is limited to32 bytes, UID to31, and password to8–63 or empty for an open network. Non-ASCII/Modified UTF-8 behavior remains unvalidated.

The protocol package also parses bounded discovery and provisioning replies. An echo acknowledgement is not proof of joining Wi-Fi. The complete acknowledgement magic and this physical board's post-reset behavior still require a controlled hardware test. Do not deploy automatic reprovisioning just because serialization tests pass.

## If credentials were erased

Use the original app on a2.4GHz network to restore connectivity until this board's factory AP behavior and provisioning cycle have been validated. Keep the notebook profile if UID, camera credentials and subnet remain unchanged; otherwise update the private profile and restart the service. It loads its profile at startup.

A future hardware validation must identify the actual configuration AP, confirm the selected physical camera, send only through an explicitly selected auxiliary interface, return to the home LAN, authenticate and receive fresh frames. No factory reset was performed as part of the implementation. Erased credentials cannot be recovered from the current supervisor.
