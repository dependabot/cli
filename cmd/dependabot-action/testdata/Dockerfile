FROM alpine:3.23 AS base
COPY update-ca-certificates /usr/local/bin/update-ca-certificates
RUN chmod 755 /usr/local/bin/update-ca-certificates

FROM base AS proxy
RUN apk add --no-cache busybox-extras
COPY proxy /dependabot-proxy
RUN chmod 755 /dependabot-proxy && mkdir -p /srv

FROM base AS updater
RUN adduser -D dependabot && mkdir -p /home/dependabot/dependabot-updater
COPY run /home/dependabot/bin/run
RUN chmod 755 /home/dependabot/bin/run
USER dependabot
WORKDIR /home/dependabot
