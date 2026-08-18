# Every TPL_* variable is an operator-supplied template input, injected by the
# provisioning runner as `lxc exec --env`. The harness runs under `set -u` and
# the image builder runs this same payload with no inputs at all, so each one
# is defaulted here and never assumed present.
TPL_SITE_TITLE="${TPL_SITE_TITLE:-}"
TPL_ADMIN_EMAIL="${TPL_ADMIN_EMAIL:-}"
TPL_ADMIN_USER="${TPL_ADMIN_USER:-admin}"
TPL_ADMIN_PASSWORD="${TPL_ADMIN_PASSWORD:-}"
TPL_LANGUAGE="${TPL_LANGUAGE:-en_US}"
TPL_INSTALL_WOOCOMMERCE="${TPL_INSTALL_WOOCOMMERCE:-false}"
TPL_DEMO_CONTENT="${TPL_DEMO_CONTENT:-false}"
TPL_PREVIEW_URL="${TPL_PREVIEW_URL:-http://localhost:8080}"

# Ubuntu 24.04 ships PHP 8.3 in its own repositories, so no third-party PPA
# is needed. Keep the extension list to what WordPress and WooCommerce
# actually require; each one costs disk on a small host.
echo "--- installing PHP 8.3 and MariaDB ---"
apt_retry update -qq
apt_retry install -y -qq \
    php8.3-cli php8.3-mysql php8.3-curl php8.3-gd php8.3-mbstring \
    php8.3-xml php8.3-zip php8.3-intl php8.3-bcmath php8.3-soap \
    mariadb-server mariadb-client \
    unzip less

# MariaDB must come back on its own after a container reboot. The Debian
# package ships the unit; enabling it is what makes it a boot service.
echo "--- enabling MariaDB ---"
systemctl enable mariadb
systemctl start mariadb || service mariadb start

# Wait for the socket rather than sleeping a fixed amount: a cold first start
# on a 1 vCPU host can take a while.
for _ in $(seq 1 60); do
    if mariadb-admin ping --silent 2>/dev/null || mysqladmin ping --silent 2>/dev/null; then
        break
    fi
    sleep 2
done

# Container-local database. Root authenticates over the unix socket, so there
# is no password to manage; nothing outside the container can reach it.
echo "--- creating the wordpress database ---"
mysql -u root -e "CREATE DATABASE IF NOT EXISTS wordpress DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"

if [ -x /usr/local/bin/composer ]; then
    echo "Composer already installed - skipping"
else
    echo "--- installing Composer ---"
    curl -fsSL https://getcomposer.org/installer -o /tmp/composer-setup.php
    php /tmp/composer-setup.php --quiet --install-dir=/usr/local/bin --filename=composer
    rm -f /tmp/composer-setup.php
fi

if [ -x /usr/local/bin/wp ]; then
    echo "WP-CLI already installed - skipping"
else
    echo "--- installing WP-CLI ---"
    curl -fsSL https://raw.githubusercontent.com/wp-cli/builds/gh-pages/phar/wp-cli.phar \
        -o /usr/local/bin/wp
    chmod 0755 /usr/local/bin/wp
fi

# WP-CLI refuses to run as root without this flag. The container IS root-only,
# so make it the default instead of teaching every caller to pass it.
cat > /usr/local/bin/wp-root <<'WRAPPER'
#!/bin/sh
exec /usr/local/bin/wp --allow-root "$@"
WRAPPER
chmod 0755 /usr/local/bin/wp-root

# Shorthand for this script only. Every WordPress call below goes through it,
# so the path and the root flag are stated once.
wpx() {
    wp --allow-root --path=/workspace/public "$@"
}

# WordPress core lands in the durable workspace, so it survives container
# replacement. Never clobber an existing installation.
mkdir -p /workspace/public
if [ ! -f /workspace/public/wp-load.php ]; then
    echo "--- downloading WordPress core ---"
    wpx core download
else
    echo "WordPress core already present in /workspace/public - skipping download"
fi

if [ ! -f /workspace/public/wp-config.php ]; then
    echo "--- writing wp-config.php ---"
    wpx config create --dbname=wordpress --dbuser=root --dbhost=localhost --skip-check
fi

# The preview sits behind Caddy TLS: without these, WordPress guesses http://
# URLs for its assets, the browser blocks them as mixed content, and even the
# installer renders unstyled. Dynamic WP_HOME lets the same install answer on
# localhost:8080 and on the public preview host.
if ! grep -q "REMOTE_PREVIEW_PROXY" /workspace/public/wp-config.php; then
    echo "--- adding preview-proxy settings to wp-config.php ---"
    python3 - <<'PYPROXY'
p = "/workspace/public/wp-config.php"
s = open(p).read()
block = """<?php
/* REMOTE_PREVIEW_PROXY: trust Caddy's forwarded proto/host (added by the wordpress template). */
if (isset($_SERVER['HTTP_X_FORWARDED_PROTO']) && $_SERVER['HTTP_X_FORWARDED_PROTO'] === 'https') {
    $_SERVER['HTTPS'] = 'on';
}
if (!defined('WP_HOME') && !empty($_SERVER['HTTP_HOST'])) {
    $remote_scheme = (!empty($_SERVER['HTTPS']) && $_SERVER['HTTPS'] !== 'off') ? 'https' : 'http';
    define('WP_HOME', $remote_scheme . '://' . $_SERVER['HTTP_HOST']);
    define('WP_SITEURL', WP_HOME);
}
"""
s = block + (s[len("<?php"):] if s.startswith("<?php") else s)
open(p, "w").write(s)
PYPROXY
fi

# php -S only serves paths that exist on disk, so pretty permalinks would 404
# without a router. This one hands anything that is not a real file to
# WordPress's front controller. It lives outside the document root so it is
# not itself reachable over HTTP.
echo "--- installing the preview server router ---"
cat > /usr/local/share/remote-wordpress-router.php <<'ROUTER'
<?php
/* Router for PHP's built-in server, installed by the remote.futrx wordpress
   template. Real files are served as-is; everything else goes to WordPress. */
$root = realpath($_SERVER['DOCUMENT_ROOT']);
$path = parse_url($_SERVER['REQUEST_URI'], PHP_URL_PATH);
$path = ($path === null || $path === false) ? '/' : rawurldecode($path);
$target = realpath($root . $path);
if ($root !== false && $target !== false && strpos($target, $root) === 0) {
    if (is_file($target)) {
        return false; // hand it back to the built-in server
    }
    if (is_dir($target) && is_file($target . '/index.php')) {
        require $target . '/index.php';
        return true;
    }
}
require $root . '/index.php';
ROUTER
chmod 0644 /usr/local/share/remote-wordpress-router.php

# php -S is enough for a single-developer preview and costs far less memory
# than nginx + php-fpm on a 4 GB box. PHP_CLI_SERVER_WORKERS keeps wp-admin
# usable: WordPress makes loopback requests to itself, which deadlock a
# single-worker built-in server.
echo "--- installing the preview web server unit ---"
cat > /etc/systemd/system/remote-wordpress.service <<'UNIT'
[Unit]
Description=WordPress preview server (PHP built-in, port 8080)
After=network.target mariadb.service
Wants=mariadb.service

[Service]
Type=simple
Environment=PHP_CLI_SERVER_WORKERS=4
WorkingDirectory=/workspace/public
ExecStart=/usr/bin/php -S 0.0.0.0:8080 -t /workspace/public /usr/local/share/remote-wordpress-router.php
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable remote-wordpress.service
systemctl restart remote-wordpress.service || true

# ---------------------------------------------------------------------------
# Site setup. Everything below is guarded so a re-run (a failed install, a
# recycled container, a workspace that outlived its rootfs) is a no-op rather
# than a reinstall. The admin password is never echoed: this whole run is teed
# into /var/log/remote-template.log inside the container.
# ---------------------------------------------------------------------------

if wpx core is-installed 2>/dev/null; then
    echo "WordPress is already installed - skipping site setup"
elif [ -z "$TPL_ADMIN_PASSWORD" ] || [ -z "$TPL_ADMIN_EMAIL" ]; then
    # No credentials: this is the image bake (or a project created before the
    # template collected inputs). Leave the site uninstalled rather than
    # inventing an administrator; the per-project run below will do it.
    echo "no admin credentials supplied - leaving WordPress uninstalled"
else
    # A title is required by the installer and every project has a name, so
    # this only catches metadata that predates the input.
    [ -n "$TPL_SITE_TITLE" ] || TPL_SITE_TITLE="$(hostname)"
    echo "--- installing WordPress at $TPL_PREVIEW_URL ---"
    wpx core install \
        --url="$TPL_PREVIEW_URL" \
        --title="$TPL_SITE_TITLE" \
        --admin_user="$TPL_ADMIN_USER" \
        --admin_password="$TPL_ADMIN_PASSWORD" \
        --admin_email="$TPL_ADMIN_EMAIL" \
        --skip-email
    echo "WordPress installed; the admin password is the WP_ADMIN_PASSWORD project secret"
fi

if wpx core is-installed 2>/dev/null; then
    # en_US is WordPress's built-in locale and has no translation package, so
    # asking for it would fail. Every other locale is installed and activated;
    # RTL follows from the locale, no extra setting.
    if [ "$TPL_LANGUAGE" != "en_US" ] && [ -n "$TPL_LANGUAGE" ]; then
        if wpx language core is-installed "$TPL_LANGUAGE" 2>/dev/null; then
            echo "language $TPL_LANGUAGE already installed"
        else
            echo "--- installing the $TPL_LANGUAGE translation ---"
            wpx language core install "$TPL_LANGUAGE" --activate || \
                echo "warning: could not install $TPL_LANGUAGE, staying on en_US"
        fi
        wpx option update WPLANG "$TPL_LANGUAGE" || true
    fi
    if [ "$TPL_LANGUAGE" = "ar" ]; then
        # An Arabic site without a timezone reports UTC everywhere. Cairo is
        # the closest thing to a neutral default across the region.
        wpx option update timezone_string "Africa/Cairo" || true
    fi

    # /%postname%/ is what everyone expects; the router above is what makes it
    # actually resolve under php -S.
    if [ "$(wpx option get permalink_structure 2>/dev/null || true)" != "/%postname%/" ]; then
        echo "--- switching to /%postname%/ permalinks ---"
        wpx rewrite structure '/%postname%/' --hard || true
    fi

    # Neither ships anything a staging site needs, and both are the first
    # thing every operator deletes by hand.
    for plugin in hello akismet; do
        if wpx plugin is-installed "$plugin" 2>/dev/null; then
            echo "--- removing the $plugin plugin ---"
            wpx plugin delete "$plugin" || true
        fi
    done

    case "$TPL_INSTALL_WOOCOMMERCE" in
        true|1|yes|on)
            if wpx plugin is-installed woocommerce 2>/dev/null; then
                echo "WooCommerce already installed - activating"
                wpx plugin activate woocommerce || true
            else
                echo "--- installing WooCommerce ---"
                wpx plugin install woocommerce --activate
            fi
            # Store country, currency and the shop pages are deliberately left
            # at WooCommerce's defaults: they are a business decision, and the
            # setup wizard asks for them on first visit to the store admin.
            ;;
    esac

    case "$TPL_DEMO_CONTENT" in
        true|1|yes|on)
            echo "keeping the WordPress sample content"
            ;;
        *)
            if wpx post list --post_type=page --name=staging-notice --format=ids 2>/dev/null | grep -q '[0-9]'; then
                echo "staging notice page already present"
            else
                echo "--- replacing the sample content with a staging notice ---"
                # Matched by slug, never by id: on a re-run over a site the
                # operator has been using, posts 1 and 2 are their content.
                for slug in hello-world sample-page; do
                    for id in $(wpx post list --post_type=post,page --name="$slug" \
                        --post_status=any --format=ids 2>/dev/null || true); do
                        wpx post delete "$id" --force || true
                    done
                done
                staging_page="$(wpx post create \
                    --post_type=page \
                    --post_status=publish \
                    --post_title='Staging notice' \
                    --post_name=staging-notice \
                    --post_content='This is a staging site provisioned by remote.futrx. Anything published here is disposable.' \
                    --porcelain)"
                wpx option update show_on_front page || true
                wpx option update page_on_front "$staging_page" || true
            fi
            ;;
    esac
fi

echo "--- versions ---"
php --version | head -1
composer --version
wp --allow-root --version
mysql --version
