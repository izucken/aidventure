# config

Runtime settings. Zero config by default (adr/3): everything has a sane value,
and only inputs that cannot be known ahead of time are read from the environment.

| variable                   | meaning                                                             |
| -------------------------- | ------------------------------------------------------------------- |
| `AIDVENTURE_ADDR`          | listen address, default `:8080`                                     |
| `AIDVENTURE_UI_DEV_SERVER` | development only: Vite origin to proxy UI requests to. Unset serves the bundle embedded in the binary |

Data is never configured: it lives in `data/` next to the binary (adr/2).
