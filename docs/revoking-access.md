# Revoking Access

If you need to invalidate all existing access (e.g. you shared the link too broadly, or want to rotate credentials), follow these steps:

## Steps

1. **Generate a new token**

   Use any method that produces a long random string (32+ characters recommended). Examples:

   - [random.org/strings](https://www.random.org/strings/)
   - Password manager generator
   - Run in a terminal:
     ```sh
     # Linux / macOS
     openssl rand -hex 32

     # PowerShell
     -join ((1..32) | ForEach-Object { '{0:x}' -f (Get-Random -Max 16) })
     ```

2. **Update the container**

   In Unraid:
   - Go to the **Docker** tab
   - Click the AirWiggler container → **Edit**
   - Update the `ACCESS_TOKEN` field with the new value
   - Click **Apply**

   Unraid will restart the container automatically.

3. **Share the new link**

   Send the updated link to anyone who should retain access:
   ```
   https://music.yourdomain.com/?token=<new-token>
   ```

## What happens

- All existing browser cookies contain the old token value
- As soon as the container restarts with the new token, every existing cookie becomes invalid
- Anyone attempting to access the app without the new link will receive a 401
- Users who visit the new link will have a fresh cookie set automatically — no further action needed on their part

## Notes

- The token is not stored anywhere in the app — it lives only in the environment variable and in each user's browser cookie
- Revocation takes effect immediately on container restart; no data is retained between sessions
