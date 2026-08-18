# Stack: WordPress

This workspace was provisioned from the `wordpress` project template.

## What is installed

| Piece | Detail |
| --- | --- |
| PHP | 8.3 CLI (`php`) with `mysql`, `curl`, `gd`, `mbstring`, `xml`, `zip`, `intl`, `bcmath`, `soap` |
| Database | MariaDB, local only, started by systemd (`systemctl status mariadb`) |
| DB auth | root over the unix socket, **no password** — `mysql -u root` just works |
| Database name | `wordpress` (already created) |
| WP-CLI | `/usr/local/bin/wp`; use `wp --allow-root` or the `wp-root` wrapper |
| Composer | `/usr/local/bin/composer` |
| Site root | `/workspace/public` — core downloaded, `wp-config.php` written, **and the site already installed** |
| Preview server | `remote-wordpress.service`, PHP built-in server on **port 8080** |
| Router | `/usr/local/share/remote-wordpress-router.php` — makes pretty permalinks resolve under `php -S` |

## The site is already set up

The template ran `wp core install` during provisioning, using the answers the
operator gave in the new-project dialog. There is no installer wizard to
finish. What that means concretely:

- an administrator account exists; its password is the project's
  **`WP_ADMIN_PASSWORD` secret** (visible on the project's Secrets tab, and
  in the container as `$WP_ADMIN_PASSWORD`). Never print it into a file, a
  commit, or the chat;
- the site language may be Arabic, in which case the admin is RTL and the
  timezone is `Africa/Cairo`;
- permalinks are `/%postname%/`;
- Hello Dolly and Akismet are removed;
- WooCommerce may already be installed and active — check before installing
  it again. Its store country, currency and pages are left at WooCommerce's
  defaults on purpose; the setup wizard asks for them.

Read the current state instead of assuming:

```bash
wp-root --path=/workspace/public core is-installed && echo installed
wp-root --path=/workspace/public option get blogname
wp-root --path=/workspace/public option get WPLANG
wp-root --path=/workspace/public plugin list --status=active
```

## Rules that matter here

- Everything you want to keep must live under `/workspace`. The rest of the
  container filesystem is replaced whenever the workspace is upgraded — this
  provisioning re-runs automatically after that happens, and it will not
  re-install a site that is already installed.
- Run WP-CLI as `wp --allow-root ...` from `/workspace/public`, or use
  `wp-root`, which adds the flag for you.
- The database connection is `--dbhost=localhost` so PHP uses the unix socket.
  Do not switch it to `127.0.0.1`.
- Do not hardcode the site URL. `wp-config.php` derives `WP_HOME` from the
  request host, so the same install answers on `localhost:8080` and on the
  public preview host.
- The preview server is a systemd unit. After changing PHP files nothing needs
  restarting; after changing the unit, run
  `systemctl restart remote-wordpress`.
- Do not start a second server on 8080 — check with `ss -ltnp` first.

## Common tasks

```bash
cd /workspace/public

# Add WooCommerce if the project did not opt in at creation
wp --allow-root plugin install woocommerce --activate

# Switch the site language (RTL follows the locale)
wp --allow-root language core install ar --activate
wp --allow-root option update WPLANG ar

# Database access
mysql -u root wordpress
wp --allow-root db query "SELECT option_value FROM wp_options WHERE option_name='siteurl'"
```

## Previews

Port 8080 is published as a preview URL by the platform. Bind any additional
server to `0.0.0.0`, never `127.0.0.1`, or it will not be reachable.
