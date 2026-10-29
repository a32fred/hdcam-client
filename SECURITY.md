# Security

Do not include camera UID, passwords, SDK initialization strings, private profiles, raw recordings, firmware or packet captures in public issues. Keep camera and viewer credentials outside the checkout in private files.

If you find a vulnerability, report it privately to the repository owner through a private channel already established with that owner. There is no project security inbox configured yet. Do not publish credentials or working exploits in an issue.

The native destination guard is a compatibility filter for the client process, not a kernel sandbox. It does not isolate the camera itself from the internet. Browser viewing requires a separate password, with TLS for non-loopback listening. Prefer an existing private transport for access outside the home. No direct camera port forwarding is required by this client.
