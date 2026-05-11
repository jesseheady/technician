FROM golang:1.26-bookworm@sha256:252599aeb51ad60b83e4d8821802068127c528c707cb7dd7afd93be057c6011c AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /technician .

FROM node:24-slim@sha256:24dc26ef1e3c3690f27ebc4136c9c186c3133b25563ae4d7f0692e4d1fe5db0e

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    mtr-tiny \
    ca-certificates \
    libcap2-bin \
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

WORKDIR /
USER technician

EXPOSE 9590

ENTRYPOINT ["technician"]
CMD ["worker", "--config", "/etc/technician/technician.yml"]
