FROM golang:1.26-bookworm@sha256:47ce5636e9936b2c5cbf708925578ef386b4f8872aec74a67bd13a627d242b19 AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /technician .

FROM node:24-slim@sha256:03eae3ef7e88a9de535496fb488d67e02b9d96a063a8967bae657744ecd513f2

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    mtr-tiny \
    ca-certificates \
    libcap2-bin \
    gosu \
    wget \
    && rm -rf /var/lib/apt/lists/*

RUN groupadd --system --gid 1001 technician && \
    useradd --system --uid 1001 --gid technician --home-dir /home/technician --create-home --shell /usr/sbin/nologin technician

ENV PLAYWRIGHT_BROWSERS_PATH=/opt/playwright-browsers

COPY internal/playwright/scripts/ /opt/technician/playwright/
RUN cd /opt/technician/playwright && \
    npm ci && \
    npx playwright install --with-deps chromium && \
    chown -R technician:technician /opt/technician/playwright "$PLAYWRIGHT_BROWSERS_PATH"
ENV NODE_PATH=/opt/technician/playwright/node_modules

COPY --from=builder /technician /usr/local/bin/technician
RUN setcap cap_net_raw+ep /usr/local/bin/technician

RUN mkdir -p /var/lib/technician /tmp/technician/artifacts /tmp/technician-videos && \
    chown -R technician:technician /var/lib/technician /tmp/technician /tmp/technician-videos

COPY scripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

WORKDIR /

EXPOSE 9590

# Runs as root so the entrypoint can chown the data volume on first boot
# after upgrades from pre-non-root images, then drops to the technician
# user via gosu. CAP_NET_RAW is preserved as a file capability on the
# binary, so unprivileged ICMP keeps working.
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["worker", "--config", "/etc/technician/technician.yml"]
