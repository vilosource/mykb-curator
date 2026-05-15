<?php
# LocalSettings.php — mykb-curator test wiki
#
# Minimal config for the test fixture image. NOT for production use.
#
# Real bootstrap (DB tables, admin account, bot account) is done by
# the entrypoint script on first container boot — see the Dockerfile
# notes. This file declares the static settings that don't depend on
# the install step.

if ( !defined( 'MEDIAWIKI' ) ) {
    exit;
}

$wgSitename = "mykb-curator test wiki";
$wgMetaNamespace = "Test";

$wgServer = getenv( 'MW_SERVER' ) ?: "http://localhost:8181";
$wgScriptPath = "";
$wgResourceBasePath = $wgScriptPath;

# SQLite backend — disposable per-test container.
$wgDBtype = "sqlite";
$wgDBserver = "";
$wgDBname = "my_wiki";
$wgSQLiteDataDir = "/var/www/data";

# Bot rights — User:Mykb-Curator-Test (created by bootstrap.sh) is
# added to the bot group, which grants the bot flag on edits so the
# curator's HumanEditsSinceBot logic can distinguish bot vs human
# revisions.
$wgGroupPermissions['bot']['bot'] = true;
$wgGroupPermissions['bot']['edit'] = true;
$wgGroupPermissions['bot']['createpage'] = true;
$wgGroupPermissions['bot']['writeapi'] = true;

# Enable uploads — needed once RenderDiagrams uploads rendered PNGs.
$wgEnableUploads = true;
$wgUploadDirectory = "/var/www/uploads";
$wgFileExtensions = array_merge( $wgFileExtensions, [ 'png', 'svg' ] );

# Secret keys — placeholder values; the bootstrap script rewrites
# these to randomly-generated values on first boot.
$wgSecretKey = "REPLACE_BY_BOOTSTRAP";
$wgUpgradeKey = "REPLACE_BY_BOOTSTRAP";

# Allow anyone to read; only registered users can edit. Bot account
# is the only thing the curator writes as.
$wgGroupPermissions['*']['edit'] = false;
$wgGroupPermissions['user']['edit'] = true;

# Enable the API.
$wgEnableAPI = true;
$wgEnableWriteAPI = true;
