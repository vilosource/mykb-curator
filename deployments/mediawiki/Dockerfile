# MediaWiki test fixture image.
#
# Pre-installed MediaWiki with SQLite backend (no external db required),
# a baseline LocalSettings.php that enables bot rights, and a known
# bot account created on first boot.
#
# Used by integration + scenario tests via testcontainers-go.
#
# v0.0 sketch: the install + bot-creation step lands in the follow-up
# PR that wires the MediaWiki adapter. For now this Dockerfile records
# the intent and the file structure.

FROM mediawiki:1.41

# SQLite makes for a trivially-disposable container — no companion db.
# Wiki data lives in /var/www/data inside the container; testcontainers
# starts fresh each suite.

RUN install -d -o www-data -g www-data /var/www/data

COPY LocalSettings.php /var/www/html/LocalSettings.php

# Bootstrap script: install the wiki + create the bot account on
# first boot. v0.5 wires the real script; for v0.0 the image is a
# placeholder.
# COPY bootstrap.sh /usr/local/bin/bootstrap.sh
# RUN chmod +x /usr/local/bin/bootstrap.sh
# ENTRYPOINT ["/usr/local/bin/bootstrap.sh"]

EXPOSE 80
