# Notifications

AirWiggler can send push notifications to the server admin via [ntfy](https://ntfy.sh) when suspicious or notable activity is detected.

This feature is entirely optional. If `NOTIFY_URL` is not set, no notifications are sent and no tracking overhead is incurred.

---

## Admin Setup

### 1. Choose an ntfy instance

- **Public (free):** Use `https://ntfy.sh` — no account required, just pick a secret topic name
- **Self-hosted:** Install the [ntfy Unraid community app](https://unraid.net/community/apps) and use your own instance

### 2. Choose a private topic name

Your topic name acts as a secret — anyone who knows it can subscribe. Use something unguessable:

```
airwiggler-a3f8c92b1d
```

Your notification URL will be:
```
https://ntfy.sh/airwiggler-a3f8c92b1d
```

### 3. Subscribe on your devices

- Install the [ntfy app](https://ntfy.sh/#subscribe) on your phone (iOS / Android)
- Subscribe to your topic URL
- Notifications will appear like any other push notification

### 4. Configure the container

Add the following environment variables in Unraid → Edit Container:

| Env Var | Example | Description |
|---------|---------|-------------|
| `NOTIFY_URL` | `https://ntfy.sh/your-secret-topic` | ntfy topic URL to POST notifications to |
| `NOTIFY_BRUTE_FORCE_THRESHOLD` | `20` | Failed token attempts per minute before alerting (default: 20) |
| `NOTIFY_AUTH_GRANT_THRESHOLD` | `10` | New cookie grants in a rolling hour before alerting (default: 10) |

---

## Notification Events

### Possible brute-force attack

**Trigger:** `NOTIFY_BRUTE_FORCE_THRESHOLD`+ failed `/?token=<value>` attempts within a 1-minute window.

**Message:**
```
⚠️ AirWiggler: Possible brute-force detected
20+ failed token attempts in the last minute.
Consider rotating ACCESS_TOKEN if this persists.
```

**Action:** If attempts continue, rotate `ACCESS_TOKEN` (see docs/revoking-access.md). Random probes with no sustained pattern can be ignored.

---

### Link may be circulating

**Trigger:** `NOTIFY_AUTH_GRANT_THRESHOLD`+ new cookie grants within a rolling 1-hour window.

**Message:**
```
⚠️ AirWiggler: Unusual authentication activity
10+ new sessions granted in the last hour.
Your share link may be circulating beyond intended recipients.
Consider rotating ACCESS_TOKEN and re-sharing selectively.
```

**Action:** Rotate `ACCESS_TOKEN` and send the new link only to intended recipients.

## Notes

- The notification feature is entirely optional — no overhead when `NOTIFY_URL` is unset
- Notifications fire asynchronously and never block request handling
- Both counters reset naturally over time (rolling window); no restart needed to clear them
- The ntfy topic name acts as a secret — keep it unguessable and don't share it publicly
