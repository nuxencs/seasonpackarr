# check=skip=InvalidDefaultArgInFrom
# Deluge daemon for the integration harness. run.sh selects the Debian release
# and the exact deluged package version for each entry. DEBIAN_RELEASE has no
# default, so a build without it fails instead of picking a release.
ARG DEBIAN_RELEASE
FROM debian:${DEBIAN_RELEASE}-slim

ARG DEBIAN_RELEASE
ARG DELUGE_PACKAGE_VERSION

# Buster left the Debian mirrors, and its archive has expired Release files.
RUN if [ "$DEBIAN_RELEASE" = buster ]; then \
		printf '%s\n' \
			'deb http://archive.debian.org/debian buster main' \
			'deb http://archive.debian.org/debian-security buster/updates main' \
			>/etc/apt/sources.list && \
		printf '%s\n' 'Acquire::Check-Valid-Until "false";' >/etc/apt/apt.conf.d/99archive; \
	fi && \
	apt-get update && \
	DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "deluged=${DELUGE_PACKAGE_VERSION}" && \
	rm -rf /var/lib/apt/lists/*

# Same uid as the test container, so the daemon can write into its packs. Deluge
# 1.3 extracts plugin eggs below HOME.
RUN useradd --uid 1000 --home-dir /config --create-home harness
ENV HOME=/config
USER 1000:1000

COPY entrypoint.sh /usr/local/bin/harness-entrypoint
EXPOSE 58846
ENTRYPOINT ["/bin/sh", "/usr/local/bin/harness-entrypoint"]
