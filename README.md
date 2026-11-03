# potd-immich

Immich's **Random** home-screen widget (iOS and Android) shows a random photo
from your whole library, your favorites or one album, and changes it about every 20 minutes.
There's no setting for "one photo a day", nor for "only photos of the people I care about":
the whole library also brings up screenshots, receipts and people you'd rather not see on your home screen.

potd-immich is for people who want their phone to show **one photo a day of the people in their life**,
chosen automatically.
It keeps an album holding exactly one "photo of the day":
point the widget at that album and it shows the same photo all day.

Once a day it picks a random photo that shows at least one named person and nobody on your blocklist,
adds it to the album, and then removes the previous one.

Requires Immich **>= 3.2.0 and < 4.0.0**, as it uses the search filter API introduced in 3.2.0.

## How it picks

- Candidates are your own (not partner-shared) images on the timeline. Archived, locked, trashed photos and videos are excluded.
- Each candidate shows at least one named, non-hidden person who is not blocked, and no blocked person (see [Blocklist](#blocklist)).
- Photos shown in the last `POTD_AVOID_REPEAT_DAYS` days are skipped, unless that would leave nothing to pick.
- The album (an exact name match among the albums you own) is created if it's missing. The new photo is added
  before the old ones are removed, so the album never goes empty.

## Blocklist

The blocklist holds the people who must never appear in the photo of the day. Only named people can be blocked:
to block someone Immich hasn't named yet, name them in Immich first, then run `update-blocklist`.

A photo is a candidate only if it shows at least one named person who isn't blocked, and no blocked person. Photos that
show only unnamed people are never picked. An unnamed person can still appear, though, next to a named, non-blocked
person in the same photo.

People you name in Immich are allowed until you block them. Run `update-blocklist` after naming new people to block them if required.
You build the blocklist when you first set up `potd-immich` (see [step 3](#3-build-the-blocklist)) and can update it
whenever you like.

## Commands

| Command | What it does |
|---|---|
| `daemon` | Default in the container. Runs forever and picks once a day, within 15 minutes after `POTD_SCHEDULE_TIME`. If potd-immich was not running at that time, it catches up as soon as it starts. |
| `run [--dry-run]` | Picks a new photo now. `--dry-run` only logs what would change. |
| `update-blocklist` | Asks, for every newly named person, whether to block them. |

## Configuration

Every option can be set through an environment variable or a flag, and a flag takes precedence over the variable (`potd-immich <command> --help`).

| Env var | Flag | Default |
|---|---|---|
| `POTD_API_URL` | `--api-url` | `http://host.docker.internal:2283` |
| `POTD_API_KEY_FILE` | `--api-key-file` | `/run/secrets/immich_api_key` |
| `POTD_ALBUM_NAME` | `--album-name` | none, required for `run` and `daemon` |
| `POTD_AVOID_REPEAT_DAYS` | `--avoid-repeat-days` | `60` |
| `POTD_STATE_FILE` | `--state-file` | `/var/lib/potd-immich/state.json` |
| `POTD_BLOCKLIST_FILE` | `--blocklist-file` | `/var/lib/potd-immich/blocklist.yaml` |
| `POTD_SCHEDULE_TIME` | `--schedule-time` | `06:00` (daemon only) |
| `POTD_SCHEDULE_TIMEZONE` | `--schedule-timezone` | `UTC` (daemon only) |

## Install with Docker Compose

### 1. Create an API key

In Immich go to **Account Settings → API Keys → New API Key** and grant only these permissions:

`person.read`, `asset.read`, `album.read`, `album.create`, `albumAsset.create`, `albumAsset.delete`, `user.read`

(`album.create` is only needed if the tool should create the album itself.)

### 2. Set up the files

potd-immich uses two directories, which can be anywhere you like:

- a **deployment directory** for `compose.yaml`, `.env` and the API key, e.g. `/opt/potd-immich`
- a **data directory** for `state.json` and `blocklist.yaml`, e.g. `/srv/potd-immich`

Set them below, then copy the rest. The container runs as the unprivileged user **uid 65532**, so it can only read and
write files that this uid owns.

```sh
POTD_DIR=/opt/potd-immich   # deployment directory
DATA_DIR=/srv/potd-immich   # data directory

sudo mkdir -p "$POTD_DIR" "$DATA_DIR"
sudo chown 65532:65532 "$DATA_DIR"
cd "$POTD_DIR"
sudo curl -fLo compose.yaml https://raw.githubusercontent.com/massitaverna/potd-immich/main/deploy/compose.yaml
sudo curl -fLo .env https://raw.githubusercontent.com/massitaverna/potd-immich/main/deploy/.env.example
sudo nano .env      # set DATA_LOCATION to your data directory, plus POTD_ALBUM_NAME, POTD_SCHEDULE_TIMEZONE, ...
sudo nano api-key   # paste the API key, nothing else
sudo chown 65532:65532 api-key
sudo chmod 400 api-key
```

Keep `compose.yaml` and `.env` together, and run every `docker compose` command below from the deployment directory.
`DATA_LOCATION` in `.env` must name the data directory. Create and chown it before the first `docker compose` command:
otherwise Docker creates it owned by root, and the container can't write to it. The API key can live elsewhere too:
set `API_KEY_LOCATION` in `.env` to its path.

Resulting layout:

```
<deployment dir>/compose.yaml   deployment
<deployment dir>/.env           settings
<deployment dir>/api-key        secret, mounted at /run/secrets/immich_api_key
<data dir>/                     state.json and blocklist.yaml, mounted at /var/lib/potd-immich
```

### 3. Build the blocklist

```sh
sudo docker compose run --rm potd-immich update-blocklist
```

Run it again whenever you name new people in Immich. Only people it hasn't asked about before are prompted.
To unblock someone, delete their line under `blocked:` in `blocklist.yaml` in your data directory. To block someone
you were already asked about, copy their line from `catalog:` to `blocked:`.

### 4. Test with a dry run

```sh
sudo docker compose run --rm potd-immich run --dry-run
```

It logs the server version, how many people are allowed or blocked, the picked photo and the album changes it would make.

### 5. Start the daemon

```sh
sudo docker compose up -d
sudo docker compose logs -f
```

`restart: unless-stopped` brings the container back after a reboot. To force a new photo right away, use
`sudo docker compose run --rm potd-immich run`.

### Update

`POTD_VERSION` in `.env` selects the image tag, and decides what an update brings:

| `POTD_VERSION` | Follows |
|---|---|
| `latest` (default) | every new release |
| `0.1` | bug-fix releases of 0.1 only (`0.1.1`, `0.1.2`, …) |
| `0.1.0` | nothing: pinned to exactly that release |

Unless you use `latest`, you may want first to set `POTD_VERSION` in `.env` to the new release
(versions are listed under [tags](https://github.com/massitaverna/potd-immich/tags); tag `v0.2.0` means
`POTD_VERSION=0.2.0`). Then:

```sh
sudo docker compose pull && sudo docker compose up -d
```

`up -d` recreates the container only if the image changed.

### Troubleshooting

- **Connection refused or timeout to `host.docker.internal`.** The container reaches Immich through the host's published
  port. Check that Immich publishes `2283:2283` rather than `127.0.0.1:2283:2283`, and that a firewall such as ufw doesn't
  block traffic from the Docker bridge to the host. If Immich listens on another port, change `POTD_API_URL`.
- **`403 ... Missing required permission`.** The API key lacks one of the permissions listed above.
- **`permission denied` on `/run/secrets/immich_api_key` or `/var/lib/potd-immich`.** The files aren't owned by uid 65532 (step 2).

## Release

Pushing a `v*` tag runs the tests and publishes a multi-arch image (amd64 and arm64) to
`ghcr.io/massitaverna/potd-immich` with the tags `X.Y.Z`, `X.Y` and `latest`:

```sh
git tag v0.1.0 && git push origin v0.1.0
```
