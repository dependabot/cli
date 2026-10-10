FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && useradd dependabot
COPY --chmod=755 run /bin/run
