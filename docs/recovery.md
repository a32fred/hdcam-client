# Recovery after a camera reset

The supervisor retries connection attempts using the same configured camera UID. It does not reset the camera or write Wi-Fi settings.

If a factory reset erased the camera's Wi-Fi credentials, restore its connection through the original camera app on a 2.4 GHz Wi-Fi network. Recovery depends on restoring the camera's network access; erased credentials cannot be recovered by the supervisor.

Keep the notebook's existing configuration if the camera UID, credentials and subnet remain the same. If reset or reprovisioning changes those values, update the private profile and restart the service. It loads configuration at startup, not during each retry. No credentials are recovered from erased flash.
