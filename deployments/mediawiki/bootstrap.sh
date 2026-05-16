#!/bin/sh
# First-boot bootstrap for the test/dev MediaWiki.
#
# This is the SINGLE SOURCE OF TRUTH for "a curator-ready MediaWiki":
# SQLite backend, file uploads enabled (RenderDiagrams), and an
# Admin account promoted into the bot group. The scenario test
# fixture builds + runs this image instead of duplicating the
# sequence in Go.
#
# Idempotent: install runs only if LocalSettings.php is absent, so a
# persisted data volume survives restarts.
#
# Port-agnostic: $wgServer is resolved per-request from the Host
# header (or $MW_SERVER if pinned), so the same image works for the
# fixed-port docker-compose AND testcontainers' random mapped port —
# no post-start fix-up needed.
set -e

LS=/var/www/html/LocalSettings.php
MW_WIKI_NAME="${MW_WIKI_NAME:-ScenarioWiki}"
MW_ADMIN_USER="${MW_ADMIN_USER:-Admin}"
MW_ADMIN_PASS="${MW_ADMIN_PASS:-adminpassword-9999}"

if [ ! -f "$LS" ]; then
  echo "bootstrap: installing MediaWiki (SQLite)..."
  mkdir -p /var/www/data
  chown -R www-data:www-data /var/www/data

  php /var/www/html/maintenance/install.php \
    --dbtype=sqlite \
    --dbpath=/var/www/data \
    --pass="$MW_ADMIN_PASS" \
    --server="${MW_SERVER:-http://localhost}" \
    --scriptpath= \
    "$MW_WIKI_NAME" "$MW_ADMIN_USER"

  # Append overrides. The last $wgServer assignment wins, so this
  # makes the wiki port-agnostic regardless of what install.php wrote.
  cat >> "$LS" <<'PHP'

# --- curator harness overrides ---
$wgEnableUploads = true;
$wgServer = getenv( 'MW_SERVER' ) ?: ( ( isset( $_SERVER['HTTP_HOST'] ) ? 'http://' . $_SERVER['HTTP_HOST'] : 'http://localhost' ) );
PHP

  chown -R www-data:www-data /var/www/data "$LS"
  chmod 644 "$LS"

  # Promote Admin into the bot group (the curator asserts bot rights
  # on every edit). --force promotes an existing user in place.
  php /var/www/html/maintenance/createAndPromote.php \
    --bot --force "$MW_ADMIN_USER" "$MW_ADMIN_PASS"

  echo "bootstrap: install complete."
else
  echo "bootstrap: LocalSettings.php present — skipping install."
fi

exec apache2-foreground
